package fonts

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/goregular"
)

func TestFamilyFromInputAcceptsGoogleFontLinksAndCSSLinks(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "https://fonts.google.com/specimen/Roboto+Mono", want: "Roboto Mono"},
		{input: "https://fonts.googleapis.com/css2?family=Open+Sans:wght@400;700&display=swap", want: "Open Sans"},
		{input: "Lobster", want: "Lobster"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, pageURL, err := FamilyFromInput(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got family %q, want %q", got, tt.want)
			}
			if !strings.HasPrefix(pageURL, GoogleFontsURL+"/specimen/") {
				t.Fatalf("got page URL %q, want Google Fonts specimen URL", pageURL)
			}
		})
	}
}

func TestFamilyFromInputRejectsNonGoogleURLs(t *testing.T) {
	_, _, err := FamilyFromInput("https://example.com/Roboto")
	if err == nil {
		t.Fatal("expected unsupported URL error")
	}
	if !strings.Contains(err.Error(), "unsupported font URL host") {
		t.Fatalf("got %q, want unsupported host error", err)
	}
}

func TestCacheAddListAndLoad(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ofl/roboto/METADATA.pb", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `name: "Roboto"
fonts {
  name: "Roboto"
  style: "normal"
  weight: 400
  filename: "Roboto-Regular.ttf"
  full_name: "Roboto Regular"
}
fonts {
  name: "Roboto"
  style: "italic"
  weight: 700
  filename: "Roboto-BoldItalic.ttf"
  full_name: "Roboto Bold Italic"
}
`)
	})
	mux.HandleFunc("/ofl/roboto/Roboto-Regular.ttf", func(w http.ResponseWriter, r *http.Request) {
		w.Write(goregular.TTF)
	})
	mux.HandleFunc("/ofl/roboto/Roboto-BoldItalic.ttf", func(w http.ResponseWriter, r *http.Request) {
		w.Write(goregular.TTF)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cache := Cache{
		Dir:        t.TempDir(),
		Client:     server.Client(),
		RawBaseURL: server.URL,
		Now: func() time.Time {
			return time.Unix(10, 0).UTC()
		},
	}

	record, err := cache.Add(context.Background(), "https://fonts.google.com/specimen/Roboto")
	if err != nil {
		t.Fatal(err)
	}
	if record.Name != "Roboto" || record.URL != "https://fonts.google.com/specimen/Roboto" {
		t.Fatalf("got record %#v, want Roboto with specimen URL", record)
	}
	if len(record.Faces) != 2 {
		t.Fatalf("got %d faces, want 2", len(record.Faces))
	}

	records, err := cache.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Name != "Roboto" {
		t.Fatalf("got records %#v, want one Roboto record", records)
	}

	_, font, err := cache.Load("roboto")
	if err != nil {
		t.Fatal(err)
	}
	if font == nil {
		t.Fatal("loaded font is nil")
	}

	_, face, font, err := cache.LoadFace("roboto", FaceRequest{Weight: 700, Italic: true})
	if err != nil {
		t.Fatal(err)
	}
	if font == nil {
		t.Fatal("loaded italic font is nil")
	}
	if face.Style != "italic" || face.Weight != 700 {
		t.Fatalf("got face %#v, want italic 700", face)
	}
	if summary := FaceSummary(record); !strings.Contains(summary, "normal 400") || !strings.Contains(summary, "italic 700") {
		t.Fatalf("got summary %q, want normal and italic weights", summary)
	}
}

func TestVariableWeightMetadataIsPreserved(t *testing.T) {
	meta, err := parseMetadata([]byte(`name: "Roboto"
fonts {
  name: "Roboto"
  style: "normal"
  weight: 400
  filename: "Roboto[wght].ttf"
}
axes {
  tag: "wght"
  min_value: 100.0
  max_value: 900.0
}
`))
	if err != nil {
		t.Fatal(err)
	}
	minWeight, maxWeight := meta.weightRange()
	if minWeight != 100 || maxWeight != 900 {
		t.Fatalf("got range %d-%d, want 100-900", minWeight, maxWeight)
	}
}
