//go:build darwin && cgo

#import <Foundation/Foundation.h>
#import <IOBluetooth/IOBluetooth.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

enum {
	LWBT_EpsonVendorID = 0x0430,
	LWBT_LW600PProductID = 0x0211,
	LWBT_DefaultReadTimeoutMS = 100,
	LWBT_ChannelCloseTimeoutMS = 750,
	LWBT_DeviceCloseTimeoutMS = 2000,
	LWBT_OpenFailureSettleMS = 500,
};

@interface LW600PRFCOMMDelegate : NSObject <IOBluetoothRFCOMMChannelDelegate> {
	NSCondition *condition;
	NSMutableData *buffer;
	BOOL closed;
}
- (NSInteger)readBytes:(void *)dst maxLength:(NSUInteger)maxLength timeoutMS:(NSInteger)timeoutMS closed:(BOOL *)closedOut;
- (void)markClosed;
@end

@implementation LW600PRFCOMMDelegate
- (instancetype)init {
	self = [super init];
	if (self) {
		condition = [[NSCondition alloc] init];
		buffer = [[NSMutableData alloc] init];
		closed = NO;
	}
	return self;
}

- (void)dealloc {
	[condition release];
	[buffer release];
	[super dealloc];
}

- (void)rfcommChannelData:(IOBluetoothRFCOMMChannel *)rfcommChannel data:(void *)dataPointer length:(size_t)dataLength {
	(void)rfcommChannel;
	if (dataPointer == NULL || dataLength == 0) {
		return;
	}
	[condition lock];
	[buffer appendBytes:dataPointer length:dataLength];
	[condition signal];
	[condition unlock];
}

- (void)rfcommChannelClosed:(IOBluetoothRFCOMMChannel *)rfcommChannel {
	(void)rfcommChannel;
	[self markClosed];
}

- (void)markClosed {
	[condition lock];
	closed = YES;
	[condition broadcast];
	[condition unlock];
}

- (NSInteger)readBytes:(void *)dst maxLength:(NSUInteger)maxLength timeoutMS:(NSInteger)timeoutMS closed:(BOOL *)closedOut {
	if (dst == NULL || maxLength == 0) {
		return 0;
	}

	NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:((double)timeoutMS) / 1000.0];
	[condition lock];
	while ([buffer length] == 0 && !closed) {
		NSDate *now = [NSDate date];
		if ([now compare:deadline] != NSOrderedAscending) {
			break;
		}

		NSTimeInterval remaining = [deadline timeIntervalSinceDate:now];
		NSTimeInterval slice = remaining < 0.05 ? remaining : 0.05;
		[condition unlock];
		[[NSRunLoop currentRunLoop] runMode:NSDefaultRunLoopMode beforeDate:[NSDate dateWithTimeIntervalSinceNow:slice]];
		[condition lock];
	}

	if ([buffer length] == 0) {
		if (closedOut != NULL) {
			*closedOut = closed;
		}
		[condition unlock];
		return 0;
	}

	NSUInteger n = maxLength < [buffer length] ? maxLength : [buffer length];
	memcpy(dst, [buffer bytes], n);
	[buffer replaceBytesInRange:NSMakeRange(0, n) withBytes:NULL length:0];
	if (closedOut != NULL) {
		*closedOut = closed;
	}
	[condition unlock];
	return (NSInteger)n;
}
@end

typedef struct {
	IOBluetoothDevice *device;
	IOBluetoothRFCOMMChannel *channel;
	LW600PRFCOMMDelegate *delegate;
} LWBTConn;

static void lwbt_set_error(char **out, const char *message) {
	if (out == NULL) {
		return;
	}
	if (message == NULL) {
		message = "unknown IOBluetooth error";
	}
	*out = strdup(message);
}

static void lwbt_set_errorf(char **out, const char *format, ...) {
	if (out == NULL) {
		return;
	}
	char buf[512];
	va_list args;
	va_start(args, format);
	vsnprintf(buf, sizeof(buf), format, args);
	va_end(args);
	*out = strdup(buf);
}

static NSString *lwbt_normalized_address(const char *address) {
	NSString *s = [NSString stringWithUTF8String:address];
	return [[s uppercaseString] stringByReplacingOccurrencesOfString:@":" withString:@"-"];
}

static IOBluetoothDevice *lwbt_device_for_address(const char *address) {
	NSString *hyphenated = lwbt_normalized_address(address);
	IOBluetoothDevice *device = [IOBluetoothDevice deviceWithAddressString:hyphenated];
	if (device != nil) {
		return device;
	}

	NSString *colon = [hyphenated stringByReplacingOccurrencesOfString:@"-" withString:@":"];
	return [IOBluetoothDevice deviceWithAddressString:colon];
}

static NSString *lwbt_colon_address(IOBluetoothDevice *device) {
	NSString *address = [device addressString];
	if (address == nil) {
		return nil;
	}
	return [[address uppercaseString] stringByReplacingOccurrencesOfString:@"-" withString:@":"];
}

static void lwbt_run_loop_for_ms(NSInteger timeoutMS) {
	if (timeoutMS <= 0) {
		return;
	}
	NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:((double)timeoutMS) / 1000.0];
	while ([[NSDate date] compare:deadline] == NSOrderedAscending) {
		[[NSRunLoop currentRunLoop] runMode:NSDefaultRunLoopMode beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.05]];
	}
}

static void lwbt_close_channel(IOBluetoothRFCOMMChannel *channel) {
	if (channel == nil) {
		return;
	}
	(void)[channel closeChannel];
	NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:((double)LWBT_ChannelCloseTimeoutMS) / 1000.0];
	while ([channel isOpen] && [[NSDate date] compare:deadline] == NSOrderedAscending) {
		[[NSRunLoop currentRunLoop] runMode:NSDefaultRunLoopMode beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.05]];
	}
	[channel setDelegate:nil];
}

static void lwbt_reset_device_connection(IOBluetoothDevice *device, NSInteger settleMS) {
	if (device == nil) {
		return;
	}
	(void)[device closeConnection];
	NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:((double)LWBT_DeviceCloseTimeoutMS) / 1000.0];
	while ([device isConnected] && [[NSDate date] compare:deadline] == NSOrderedAscending) {
		[[NSRunLoop currentRunLoop] runMode:NSDefaultRunLoopMode beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.05]];
	}
	lwbt_run_loop_for_ms(settleMS);
}

static unsigned int lwbt_unsigned_attribute(IOBluetoothSDPServiceRecord *record, BluetoothSDPServiceAttributeID attributeID, BOOL *okOut) {
	if (okOut != NULL) {
		*okOut = NO;
	}
	IOBluetoothSDPDataElement *element = [record getAttributeDataElement:attributeID];
	if (element == nil) {
		return 0;
	}
	NSNumber *number = [element getNumberValue];
	if (number == nil) {
		return 0;
	}
	if (okOut != NULL) {
		*okOut = YES;
	}
	return [number unsignedIntValue];
}

static BOOL lwbt_device_ids(IOBluetoothDevice *device, unsigned int *vendorIDOut, unsigned int *productIDOut) {
	NSArray *services = [device services];
	for (IOBluetoothSDPServiceRecord *record in services) {
		if (![record matchesUUID16:kBluetoothSDPUUID16ServiceClassPnPInformation]) {
			continue;
		}

		BOOL hasVendor = NO;
		BOOL hasProduct = NO;
		unsigned int vendorID = lwbt_unsigned_attribute(record, kBluetoothSDPAttributeDeviceIdentifierVendorID, &hasVendor);
		unsigned int productID = lwbt_unsigned_attribute(record, kBluetoothSDPAttributeDeviceIdentifierProductID, &hasProduct);
		if (!hasVendor || !hasProduct) {
			continue;
		}

		if (vendorIDOut != NULL) {
			*vendorIDOut = vendorID;
		}
		if (productIDOut != NULL) {
			*productIDOut = productID;
		}
		return YES;
	}
	return NO;
}

static NSString *lwbt_device_label(IOBluetoothDevice *device) {
	NSString *name = [device nameOrAddress];
	NSString *address = lwbt_colon_address(device);
	if (name == nil || address == nil || [name isEqualToString:address]) {
		return address == nil ? @"<unknown>" : address;
	}
	return [NSString stringWithFormat:@"%@ (%@)", name, address];
}

int lwbt_find_lw600p(char **addressOut, char **nameOut, unsigned int *vendorIDOut, unsigned int *productIDOut, char **errOut) {
	@autoreleasepool {
		NSArray *devices = [IOBluetoothDevice pairedDevices];
		NSMutableArray *matches = [NSMutableArray array];
		for (IOBluetoothDevice *device in devices) {
			unsigned int vendorID = 0;
			unsigned int productID = 0;
			if (!lwbt_device_ids(device, &vendorID, &productID)) {
				continue;
			}
			if (vendorID == LWBT_EpsonVendorID && productID == LWBT_LW600PProductID) {
				[matches addObject:device];
			}
		}

		if ([matches count] == 0) {
			lwbt_set_errorf(errOut, "no paired LW-600P found by Bluetooth Device ID vendor=0x%04x product=0x%04x; pair the printer or pass --address", LWBT_EpsonVendorID, LWBT_LW600PProductID);
			return 0;
		}
		if ([matches count] > 1) {
			NSMutableArray *labels = [NSMutableArray arrayWithCapacity:[matches count]];
			for (IOBluetoothDevice *device in matches) {
				[labels addObject:lwbt_device_label(device)];
			}
			NSString *joined = [labels componentsJoinedByString:@", "];
			lwbt_set_errorf(errOut, "multiple paired LW-600P printers match vendor=0x%04x product=0x%04x: %s; pass --address", LWBT_EpsonVendorID, LWBT_LW600PProductID, [joined UTF8String]);
			return 0;
		}

		IOBluetoothDevice *device = [matches objectAtIndex:0];
		NSString *address = lwbt_colon_address(device);
		if (address == nil) {
			lwbt_set_error(errOut, "matched LW-600P has no Bluetooth address");
			return 0;
		}
		NSString *name = [device nameOrAddress];
		if (addressOut != NULL) {
			*addressOut = strdup([address UTF8String]);
		}
		if (nameOut != NULL) {
			*nameOut = name == nil ? NULL : strdup([name UTF8String]);
		}
		if (vendorIDOut != NULL) {
			*vendorIDOut = LWBT_EpsonVendorID;
		}
		if (productIDOut != NULL) {
			*productIDOut = LWBT_LW600PProductID;
		}
		return 1;
	}
}

void *lwbt_open(const char *address, int channelID, unsigned int baud, char **errOut) {
	@autoreleasepool {
		if (![NSThread isMainThread]) {
			lwbt_set_error(errOut, "IOBluetooth RFCOMM open must run on the process main thread");
			return NULL;
		}
		if (address == NULL || strlen(address) == 0) {
			lwbt_set_error(errOut, "missing Bluetooth address");
			return NULL;
		}
		if (!RFCOMM_CHANNEL_ID_IS_VALID(channelID)) {
			lwbt_set_errorf(errOut, "invalid RFCOMM channel %d", channelID);
			return NULL;
		}

		IOBluetoothDevice *device = lwbt_device_for_address(address);
		if (device == nil) {
			lwbt_set_errorf(errOut, "Bluetooth device %s was not found", address);
			return NULL;
		}

		LW600PRFCOMMDelegate *delegate = [[LW600PRFCOMMDelegate alloc] init];
		IOBluetoothRFCOMMChannel *channel = nil;
		IOReturn status = [device openRFCOMMChannelSync:&channel withChannelID:(BluetoothRFCOMMChannelID)channelID delegate:delegate];
		if (status != kIOReturnSuccess || channel == nil) {
			[delegate markClosed];
			if (channel != nil) {
				lwbt_close_channel(channel);
				[channel release];
			}
			lwbt_reset_device_connection(device, LWBT_OpenFailureSettleMS);
			/*
			 * IOBluetooth can still deliver queued delegate callbacks after a
			 * synchronous open failure. Releasing the delegate here caused
			 * process-level SIGBUS/SIGSEGV crashes before Go could return an
			 * error. Failed opens are bounded by the Go retry loop, so keeping
			 * this small object alive for the process lifetime is safer than a
			 * use-after-free in the platform callback path.
			 */
			lwbt_set_errorf(errOut, "open RFCOMM channel %d to %s failed: IOReturn=0x%08x", channelID, address, status);
			return NULL;
		}

		if (baud > 0) {
			(void)[channel setSerialParameters:baud dataBits:8 parity:kBluetoothRFCOMMParityTypeNoParity stopBits:1];
		}

		LWBTConn *conn = calloc(1, sizeof(LWBTConn));
		if (conn == NULL) {
			[delegate markClosed];
			lwbt_close_channel(channel);
			lwbt_reset_device_connection(device, 0);
			[channel release];
			[delegate release];
			lwbt_set_error(errOut, "allocate Bluetooth connection");
			return NULL;
		}

		conn->device = [device retain];
		conn->channel = channel;
		conn->delegate = delegate;
		return conn;
	}
}

int lwbt_read(void *rawConn, unsigned char *dst, int length, int timeoutMS, int *codeOut, char **errOut) {
	if (codeOut != NULL) {
		*codeOut = 0;
	}
	LWBTConn *conn = (LWBTConn *)rawConn;
	if (conn == NULL || conn->delegate == nil) {
		if (codeOut != NULL) {
			*codeOut = 2;
		}
		lwbt_set_error(errOut, "Bluetooth connection is closed");
		return 0;
	}
	if (length <= 0) {
		return 0;
	}
	if (timeoutMS <= 0) {
		timeoutMS = LWBT_DefaultReadTimeoutMS;
	}

	BOOL closedOut = NO;
	NSInteger n = [conn->delegate readBytes:dst maxLength:(NSUInteger)length timeoutMS:(NSInteger)timeoutMS closed:&closedOut];
	if (closedOut && n == 0 && codeOut != NULL) {
		*codeOut = 1;
	}
	return (int)n;
}

int lwbt_write(void *rawConn, const unsigned char *src, int length, char **errOut) {
	LWBTConn *conn = (LWBTConn *)rawConn;
	if (conn == NULL || conn->channel == nil) {
		lwbt_set_error(errOut, "Bluetooth connection is closed");
		return -1;
	}
	if (src == NULL || length <= 0) {
		return 0;
	}

	BluetoothRFCOMMMTU mtu = [conn->channel getMTU];
	if (mtu == 0) {
		mtu = 127;
	}

	int written = 0;
	while (written < length) {
		int remaining = length - written;
		UInt16 chunk = (UInt16)(remaining < mtu ? remaining : mtu);
		IOReturn status = [conn->channel writeSync:(void *)(src + written) length:chunk];
		if (status != kIOReturnSuccess) {
			lwbt_set_errorf(errOut, "write RFCOMM data failed after %d bytes: IOReturn=0x%08x", written, status);
			return -1;
		}
		written += chunk;
	}
	return written;
}

int lwbt_close(void *rawConn, char **errOut) {
	(void)errOut;
	LWBTConn *conn = (LWBTConn *)rawConn;
	if (conn == NULL) {
		return 0;
	}
	/*
	 * Recent macOS IOBluetooth builds can crash in objc_msgSend while
	 * synchronously tearing down an RFCOMM channel after a successful print.
	 * The CLI exits immediately after Close, so leave the Objective-C channel,
	 * device, and delegate alive for process teardown instead of messaging
	 * potentially invalid framework objects here.
	 */
	free(conn);
	return 0;
}
