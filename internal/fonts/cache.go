package fonts

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/font/opentype"
)

const (
	GoogleFontsURL = "https://fonts.google.com"
	DefaultName    = "Roboto"

	defaultRawBaseURL = "https://raw.githubusercontent.com/google/fonts/main"
	manifestFilename  = "fonts.json"
	maxMetadataBytes  = 1 << 20
	maxFontBytes      = 20 << 20
)

var ErrNoFonts = errors.New("no fonts downloaded")

type NotFoundError struct {
	Name string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("font %q is not downloaded", e.Name)
}

type Cache struct {
	Dir        string
	Client     *http.Client
	RawBaseURL string
	Now        func() time.Time
}

type Font struct {
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	File        string    `json:"file"`
	DownloadURL string    `json:"download_url"`
	Downloaded  time.Time `json:"downloaded_at"`
	Faces       []Face    `json:"faces,omitempty"`
}

type Face struct {
	Style       string `json:"style"`
	Weight      int    `json:"weight"`
	WeightMin   int    `json:"weight_min,omitempty"`
	WeightMax   int    `json:"weight_max,omitempty"`
	File        string `json:"file"`
	DownloadURL string `json:"download_url"`
	FullName    string `json:"full_name,omitempty"`
}

type FaceRequest struct {
	Weight int
	Italic bool
}

type manifest struct {
	Fonts []Font `json:"fonts"`
}

type repositoryFont struct {
	Style    string
	Weight   int
	Filename string
	FullName string
}

type repositoryMetadata struct {
	Name  string
	Fonts []repositoryFont
	Axes  []repositoryAxis
}

type repositoryAxis struct {
	Tag string
	Min float64
	Max float64
}

type httpStatusError struct {
	URL    string
	Status int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s returned HTTP %d", e.URL, e.Status)
}

func DefaultCache() (Cache, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return Cache{}, fmt.Errorf("resolve user cache directory: %w", err)
	}
	return Cache{Dir: filepath.Join(dir, "nospero", "fonts")}, nil
}

func (c Cache) Add(ctx context.Context, input string) (Font, error) {
	c = c.withDefaults()
	family, pageURL, err := FamilyFromInput(input)
	if err != nil {
		return Font{}, err
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return Font{}, fmt.Errorf("create font cache directory: %w", err)
	}

	meta, licenseDir, err := c.downloadFamilyMetadata(ctx, family)
	if err != nil {
		return Font{}, err
	}
	name := meta.Name
	if name == "" {
		name = family
	}
	record := Font{
		Name:       name,
		URL:        pageURL,
		Downloaded: c.Now(),
	}

	for _, sourceFace := range meta.Fonts {
		if err := validateRepositoryFilename(sourceFace.Filename); err != nil {
			return Font{}, err
		}
		fontURL := c.rawURL(licenseDir, familySlug(family), sourceFace.Filename)
		fontData, err := c.get(ctx, fontURL, maxFontBytes)
		if err != nil {
			return Font{}, err
		}
		if _, err := opentype.Parse(fontData); err != nil {
			return Font{}, fmt.Errorf("downloaded font %q is not a renderable TTF/OTF: %w", sourceFace.Filename, err)
		}

		face := Face{
			Style:       normalizeStyle(sourceFace.Style),
			Weight:      normalizedWeight(sourceFace.Weight),
			File:        cacheFilename(familySlug(name), sourceFace.Filename),
			DownloadURL: fontURL,
			FullName:    sourceFace.FullName,
		}
		if meta.hasWeightAxis() && strings.Contains(sourceFace.Filename, "wght") {
			face.WeightMin, face.WeightMax = meta.weightRange()
		}
		if err := writeFileAtomic(filepath.Join(c.Dir, face.File), fontData, 0o644); err != nil {
			return Font{}, fmt.Errorf("write cached font: %w", err)
		}
		record.Faces = append(record.Faces, face)
	}
	defaultFace, err := record.defaultFace()
	if err != nil {
		return Font{}, err
	}
	record.File = defaultFace.File
	record.DownloadURL = defaultFace.DownloadURL

	records, err := c.List()
	if err != nil {
		return Font{}, err
	}
	records = upsertFont(records, record)
	if err := c.writeManifest(records); err != nil {
		return Font{}, err
	}
	return record, nil
}

func (c Cache) List() ([]Font, error) {
	c = c.withDefaults()
	m, err := c.readManifest()
	if err != nil {
		return nil, err
	}
	out := make([]Font, 0, len(m.Fonts))
	for _, record := range m.Fonts {
		out = append(out, normalizeFont(record))
	}
	sort.Slice(out, func(i, j int) bool {
		return fontKey(out[i].Name) < fontKey(out[j].Name)
	})
	return out, nil
}

func (c Cache) Load(name string) (Font, *opentype.Font, error) {
	record, _, font, err := c.LoadFace(name, FaceRequest{Weight: 400})
	return record, font, err
}

func (c Cache) LoadFace(name string, request FaceRequest) (Font, Face, *opentype.Font, error) {
	c = c.withDefaults()
	records, err := c.List()
	if err != nil {
		return Font{}, Face{}, nil, err
	}
	if len(records) == 0 {
		return Font{}, Face{}, nil, ErrNoFonts
	}
	key := fontKey(name)
	for _, record := range records {
		if fontKey(record.Name) != key {
			continue
		}
		face, err := record.selectFace(request)
		if err != nil {
			return Font{}, Face{}, nil, err
		}
		data, err := readCachedFont(filepath.Join(c.Dir, face.File))
		if err != nil {
			return Font{}, Face{}, nil, err
		}
		font, err := opentype.Parse(data)
		if err != nil {
			return Font{}, Face{}, nil, fmt.Errorf("cached font %q is not renderable; run nospero fonts add again: %w", record.Name, err)
		}
		return record, face, font, nil
	}
	return Font{}, Face{}, nil, &NotFoundError{Name: name}
}

func FamilyFromInput(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("font URL or family name is required")
	}

	family := input
	if u, err := url.Parse(input); err == nil && u.Scheme != "" {
		extracted, err := familyFromURL(u)
		if err != nil {
			return "", "", err
		}
		family = extracted
	}

	family, err := cleanFamilyName(family)
	if err != nil {
		return "", "", err
	}
	return family, specimenURL(family), nil
}

func (c Cache) withDefaults() Cache {
	if c.Client == nil {
		c.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if c.RawBaseURL == "" {
		c.RawBaseURL = defaultRawBaseURL
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

func (c Cache) downloadFamilyMetadata(ctx context.Context, family string) (repositoryMetadata, string, error) {
	slug := familySlug(family)
	if slug == "" {
		return repositoryMetadata{}, "", fmt.Errorf("font family %q cannot be mapped to Google Fonts repository path", family)
	}

	for _, licenseDir := range []string{"ofl", "apache", "ufl"} {
		metadataURL := c.rawURL(licenseDir, slug, "METADATA.pb")
		metadataBytes, err := c.get(ctx, metadataURL, maxMetadataBytes)
		if isHTTPStatus(err, http.StatusNotFound) {
			continue
		}
		if err != nil {
			return repositoryMetadata{}, "", err
		}
		meta, err := parseMetadata(metadataBytes)
		if err != nil {
			return repositoryMetadata{}, "", fmt.Errorf("parse Google Fonts metadata for %q: %w", family, err)
		}
		return meta, licenseDir, nil
	}
	return repositoryMetadata{}, "", fmt.Errorf("font %q was not found in Google Fonts; check the family name from %s", family, GoogleFontsURL)
}

func (c Cache) rawURL(licenseDir, slug, filename string) string {
	base := strings.TrimRight(c.RawBaseURL, "/")
	parts := []string{licenseDir, slug, filename}
	for _, part := range parts {
		base += "/" + url.PathEscape(part)
	}
	return base
}

func (c Cache) get(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &httpStatusError{URL: rawURL, Status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download from %s exceeded %d bytes", rawURL, limit)
	}
	return data, nil
}

func (c Cache) readManifest() (manifest, error) {
	data, err := os.ReadFile(filepath.Join(c.Dir, manifestFilename))
	if errors.Is(err, os.ErrNotExist) {
		return manifest{}, nil
	}
	if err != nil {
		return manifest{}, fmt.Errorf("read font cache manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return manifest{}, fmt.Errorf("parse font cache manifest: %w", err)
	}
	return m, nil
}

func (c Cache) writeManifest(records []Font) error {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("create font cache directory: %w", err)
	}
	sort.Slice(records, func(i, j int) bool {
		return fontKey(records[i].Name) < fontKey(records[j].Name)
	})
	data, err := json.MarshalIndent(manifest{Fonts: records}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := writeFileAtomic(filepath.Join(c.Dir, manifestFilename), data, 0o644); err != nil {
		return fmt.Errorf("write font cache manifest: %w", err)
	}
	return nil
}

func parseMetadata(data []byte) (repositoryMetadata, error) {
	var meta repositoryMetadata
	var current repositoryFont
	var currentAxis repositoryAxis
	inFont := false
	inAxis := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "fonts {" {
			inFont = true
			current = repositoryFont{}
			continue
		}
		if line == "axes {" {
			inAxis = true
			currentAxis = repositoryAxis{}
			continue
		}
		if inFont {
			if line == "}" {
				if current.Filename != "" {
					meta.Fonts = append(meta.Fonts, current)
				}
				inFont = false
				continue
			}
			if value, ok, err := quotedField(line, "style"); err != nil {
				return repositoryMetadata{}, err
			} else if ok {
				current.Style = value
				continue
			}
			if value, ok, err := quotedField(line, "filename"); err != nil {
				return repositoryMetadata{}, err
			} else if ok {
				current.Filename = value
				continue
			}
			if value, ok, err := quotedField(line, "full_name"); err != nil {
				return repositoryMetadata{}, err
			} else if ok {
				current.FullName = value
				continue
			}
			if strings.HasPrefix(line, "weight:") {
				weight, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "weight:")))
				if err != nil {
					return repositoryMetadata{}, fmt.Errorf("parse font weight %q: %w", line, err)
				}
				current.Weight = weight
			}
			continue
		}
		if inAxis {
			if line == "}" {
				if currentAxis.Tag != "" {
					meta.Axes = append(meta.Axes, currentAxis)
				}
				inAxis = false
				continue
			}
			if value, ok, err := quotedField(line, "tag"); err != nil {
				return repositoryMetadata{}, err
			} else if ok {
				currentAxis.Tag = value
				continue
			}
			if strings.HasPrefix(line, "min_value:") {
				value, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, "min_value:")), 64)
				if err != nil {
					return repositoryMetadata{}, fmt.Errorf("parse axis minimum %q: %w", line, err)
				}
				currentAxis.Min = value
				continue
			}
			if strings.HasPrefix(line, "max_value:") {
				value, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(line, "max_value:")), 64)
				if err != nil {
					return repositoryMetadata{}, fmt.Errorf("parse axis maximum %q: %w", line, err)
				}
				currentAxis.Max = value
				continue
			}
			continue
		}
		if value, ok, err := quotedField(line, "name"); err != nil {
			return repositoryMetadata{}, err
		} else if ok && meta.Name == "" {
			meta.Name = value
		}
	}
	if err := scanner.Err(); err != nil {
		return repositoryMetadata{}, err
	}
	if meta.Name == "" {
		return repositoryMetadata{}, fmt.Errorf("metadata does not include a family name")
	}
	if len(meta.Fonts) == 0 {
		return repositoryMetadata{}, fmt.Errorf("metadata does not include font files")
	}
	return meta, nil
}

func (m repositoryMetadata) hasWeightAxis() bool {
	for _, axis := range m.Axes {
		if axis.Tag == "wght" && axis.Max > axis.Min {
			return true
		}
	}
	return false
}

func (m repositoryMetadata) weightRange() (int, int) {
	for _, axis := range m.Axes {
		if axis.Tag == "wght" && axis.Max > axis.Min {
			return int(math.Round(axis.Min)), int(math.Round(axis.Max))
		}
	}
	return 0, 0
}

func (f Font) defaultFace() (Face, error) {
	return f.selectFace(FaceRequest{Weight: 400})
}

func (f Font) selectFace(request FaceRequest) (Face, error) {
	f = normalizeFont(f)
	if len(f.Faces) == 0 {
		return Face{}, fmt.Errorf("font %q has no cached faces; run nospero fonts add again", f.Name)
	}
	return selectFace(f.Faces, request), nil
}

func selectFace(faces []Face, request FaceRequest) Face {
	if request.Weight == 0 {
		request.Weight = 400
	}
	wantStyle := "normal"
	if request.Italic {
		wantStyle = "italic"
	}

	candidates := facesByStyle(faces, wantStyle)
	if len(candidates) == 0 && request.Italic {
		candidates = facesByStyle(faces, "normal")
	}
	if len(candidates) == 0 {
		candidates = faces
	}

	best := candidates[0]
	bestScore := faceScore(best, request.Weight)
	for _, candidate := range candidates[1:] {
		score := faceScore(candidate, request.Weight)
		if score < bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func facesByStyle(faces []Face, style string) []Face {
	var out []Face
	for _, face := range faces {
		if normalizeStyle(face.Style) == style {
			out = append(out, face)
		}
	}
	return out
}

func faceScore(face Face, weight int) int {
	if face.WeightMin > 0 && face.WeightMax >= face.WeightMin {
		switch {
		case weight < face.WeightMin:
			return face.WeightMin - weight
		case weight > face.WeightMax:
			return weight - face.WeightMax
		default:
			return 0
		}
	}
	return abs(normalizedWeight(face.Weight) - weight)
}

func normalizeFont(record Font) Font {
	if len(record.Faces) == 0 && record.File != "" {
		record.Faces = []Face{{
			Style:       "normal",
			Weight:      400,
			File:        record.File,
			DownloadURL: record.DownloadURL,
		}}
	}
	for i := range record.Faces {
		record.Faces[i].Style = normalizeStyle(record.Faces[i].Style)
		record.Faces[i].Weight = normalizedWeight(record.Faces[i].Weight)
	}
	if record.File == "" && len(record.Faces) > 0 {
		face := selectFace(record.Faces, FaceRequest{Weight: 400})
		record.File = face.File
		record.DownloadURL = face.DownloadURL
	}
	return record
}

func normalizeStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "italic":
		return "italic"
	default:
		return "normal"
	}
}

func normalizedWeight(weight int) int {
	if weight <= 0 {
		return 400
	}
	return weight
}

func FaceSummary(font Font) string {
	font = normalizeFont(font)
	if len(font.Faces) == 0 {
		return ""
	}
	byStyle := map[string][]Face{}
	for _, face := range font.Faces {
		byStyle[normalizeStyle(face.Style)] = append(byStyle[normalizeStyle(face.Style)], face)
	}
	var parts []string
	for _, style := range []string{"normal", "italic"} {
		faces := byStyle[style]
		if len(faces) == 0 {
			continue
		}
		sort.Slice(faces, func(i, j int) bool {
			return normalizedWeight(faces[i].Weight) < normalizedWeight(faces[j].Weight)
		})
		weights := make([]string, 0, len(faces))
		seen := map[string]bool{}
		for _, face := range faces {
			label := strconv.Itoa(normalizedWeight(face.Weight))
			if face.WeightMin > 0 && face.WeightMax >= face.WeightMin {
				label = fmt.Sprintf("%d-%d", face.WeightMin, face.WeightMax)
			}
			if seen[label] {
				continue
			}
			seen[label] = true
			weights = append(weights, label)
		}
		parts = append(parts, style+" "+strings.Join(weights, ","))
	}
	return strings.Join(parts, "; ")
}

func (m repositoryMetadata) regularFont() (repositoryFont, error) {
	best := -1
	bestDistance := int(^uint(0) >> 1)
	for i, font := range m.Fonts {
		if font.Style != "normal" {
			continue
		}
		distance := abs(font.Weight - 400)
		if distance < bestDistance {
			best = i
			bestDistance = distance
		}
	}
	if best >= 0 {
		return m.Fonts[best], nil
	}
	if len(m.Fonts) > 0 {
		return m.Fonts[0], nil
	}
	return repositoryFont{}, fmt.Errorf("metadata for %q does not include a usable font", m.Name)
}

func quotedField(line, key string) (string, bool, error) {
	prefix := key + ":"
	if !strings.HasPrefix(line, prefix) {
		return "", false, nil
	}
	raw := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	value, err := strconv.Unquote(raw)
	if err != nil {
		return "", false, fmt.Errorf("parse %s field %q: %w", key, line, err)
	}
	return value, true, nil
}

func familyFromURL(u *url.URL) (string, error) {
	host := strings.ToLower(u.Hostname())
	switch host {
	case "fonts.google.com", "www.fonts.google.com":
		parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
		for i := 0; i+1 < len(parts); i++ {
			if parts[i] != "specimen" {
				continue
			}
			value, err := url.PathUnescape(parts[i+1])
			if err != nil {
				return "", err
			}
			return value, nil
		}
		for _, key := range []string{"family", "selection.family", "query"} {
			if value := rawQueryValue(u.RawQuery, key); value != "" {
				return value, nil
			}
		}
	case "fonts.googleapis.com":
		if value := rawQueryValue(u.RawQuery, "family"); value != "" {
			return value, nil
		}
	default:
		return "", fmt.Errorf("unsupported font URL host %q; use a %s font page or enter a family name", u.Hostname(), GoogleFontsURL)
	}
	return "", fmt.Errorf("could not find a font family in %s", u.String())
}

func rawQueryValue(rawQuery, key string) string {
	for _, field := range strings.Split(rawQuery, "&") {
		rawKey, rawValue, ok := strings.Cut(field, "=")
		if !ok || rawKey != key {
			continue
		}
		value, err := url.QueryUnescape(rawValue)
		if err != nil {
			return ""
		}
		return value
	}
	return ""
}

func cleanFamilyName(name string) (string, error) {
	name = strings.ReplaceAll(name, "+", " ")
	if before, _, ok := strings.Cut(name, ":"); ok {
		name = before
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", fmt.Errorf("font family name is empty")
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			continue
		}
		return "", fmt.Errorf("unsupported character %q in font family %q", r, name)
	}
	return name, nil
}

func specimenURL(family string) string {
	return GoogleFontsURL + "/specimen/" + strings.ReplaceAll(url.PathEscape(family), "%20", "+")
}

func familySlug(family string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(family) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validateRepositoryFilename(filename string) error {
	if filename == "" || filename != filepath.Base(filename) {
		return fmt.Errorf("unsafe font filename %q in Google Fonts metadata", filename)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".ttf" && ext != ".otf" {
		return fmt.Errorf("unsupported font file %q; expected .ttf or .otf", filename)
	}
	return nil
}

func cacheFilename(slug, filename string) string {
	var b strings.Builder
	b.WriteString(slug)
	b.WriteByte('-')
	for _, r := range filename {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_', r == '[', r == ']', r == ',':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func readCachedFont(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read cached font: %w", err)
	}
	if info.Size() > maxFontBytes {
		return nil, fmt.Errorf("cached font %s exceeds %d bytes", path, maxFontBytes)
	}
	return os.ReadFile(path)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func upsertFont(records []Font, record Font) []Font {
	key := fontKey(record.Name)
	for i := range records {
		if fontKey(records[i].Name) == key {
			records[i] = record
			return records
		}
	}
	return append(records, record)
}

func fontKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func isHTTPStatus(err error, status int) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.Status == status
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
