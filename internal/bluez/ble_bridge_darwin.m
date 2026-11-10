// ble_bridge_darwin.m is a thin CoreBluetooth central for the ATVV GATT
// service (service ab5e0001-…, TX ab5e0002-…, audio ab5e0003-…,
// control ab5e0004-…). All delegate callbacks land on a private serial
// dispatch queue; the C ABI is non-blocking and reports completion through
// atvv_event_cb. See ble_bridge_darwin.h for the contract.

#import <Foundation/Foundation.h>
#import <CoreBluetooth/CoreBluetooth.h>
#include "ble_bridge_darwin.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static NSString *const kAtvvServiceUUID = @"ab5e0001-5a21-4f05-bc7d-af01f617b664";
static NSString *const kAtvvTXUUID = @"ab5e0002-5a21-4f05-bc7d-af01f617b664";
static NSString *const kAtvvAudioUUID = @"ab5e0003-5a21-4f05-bc7d-af01f617b664";
static NSString *const kAtvvControlUUID = @"ab5e0004-5a21-4f05-bc7d-af01f617b664";
// HID over GATT: the remote is paired as a BLE HID keyboard, so it may not
// advertise ATVV while connected. Matching either service finds it.
static NSString *const kHogServiceUUID = @"1812";

static const NSTimeInterval kStateTimeout = 5.0;
static const NSTimeInterval kScanTimeout = 10.0;
static const NSTimeInterval kConnectTimeout = 20.0;
static const NSTimeInterval kDiscoverTimeout = 10.0;
static const NSTimeInterval kSubscribeTimeout = 5.0;

typedef NS_ENUM(NSInteger, AtvvPhase) {
	AtvvPhaseIdle = 0,
	AtvvPhaseResolving,   // waiting for power / candidate resolution / scan
	AtvvPhaseConnecting,  // connectPeripheral in flight
	AtvvPhaseDiscovering, // service/characteristic discovery in flight
	AtvvPhaseReady,       // GATT ready; may subscribe and write
};

static void setErr(char *buf, size_t len, const char *fmt, ...) {
	if (buf == NULL || len == 0) {
		return;
	}
	va_list args;
	va_start(args, fmt);
	vsnprintf(buf, len, fmt, args);
	va_end(args);
}

@interface AtvvBridge : NSObject <CBCentralManagerDelegate, CBPeripheralDelegate>

@property (nonatomic, strong) dispatch_queue_t queue;
@property (nonatomic, strong) CBCentralManager *central;
@property (nonatomic, strong) CBPeripheral *peripheral;
@property (nonatomic, strong) CBService *atvvService;
@property (nonatomic, strong) CBCharacteristic *txChar;
@property (nonatomic, strong) CBCharacteristic *audioChar;
@property (nonatomic, strong) CBCharacteristic *controlChar;
@property (nonatomic, copy) NSString *wantID;
@property (nonatomic, copy) NSString *wantName;
@property (nonatomic, assign) void *user;
@property (nonatomic, assign) atvv_data_cb onAudio;
@property (nonatomic, assign) atvv_data_cb onControl;
@property (nonatomic, assign) atvv_event_cb onEvent;
@property (nonatomic, assign) BOOL closing;
@property (nonatomic, assign) AtvvPhase phase;
@property (nonatomic, assign) NSInteger generation;
@property (nonatomic, assign) BOOL audioNotifying;
@property (nonatomic, assign) BOOL controlNotifying;
@property (nonatomic, assign) char *identifierC;

- (instancetype)initWithID:(NSString *)wantID
	nameHint:(NSString *)wantName
	user:(void *)user
	onAudio:(atvv_data_cb)onAudio
	onControl:(atvv_data_cb)onControl
	onEvent:(atvv_event_cb)onEvent;
- (void)startConnect;
- (void)startSubscribe;
- (int)writeTX:(const uint8_t *)data length:(size_t)len errbuf:(char *)errbuf errlen:(size_t)errlen;
- (const char *)identifierCString;
- (void)teardown;

@end

@implementation AtvvBridge

- (instancetype)initWithID:(NSString *)wantID
	nameHint:(NSString *)wantName
	user:(void *)user
	onAudio:(atvv_data_cb)onAudio
	onControl:(atvv_data_cb)onControl
	onEvent:(atvv_event_cb)onEvent
{
	self = [super init];
	if (self == nil) {
		return nil;
	}
	_wantID = [wantID copy];
	_wantName = [wantName copy];
	_user = user;
	_onAudio = onAudio;
	_onControl = onControl;
	_onEvent = onEvent;
	_phase = AtvvPhaseIdle;
	_queue = dispatch_queue_create("org.open-voice-bridge.atvv", DISPATCH_QUEUE_SERIAL);
	_central = [[CBCentralManager alloc] initWithDelegate:self queue:_queue];
	return self;
}

#pragma mark - helpers (queue only)

- (void)bump {
	self.generation += 1;
}

- (void)emit:(int)event message:(NSString *)message {
	if (self.closing || self.onEvent == NULL) {
		return;
	}
	self.onEvent(event, message != nil ? message.UTF8String : NULL, self.user);
}

- (void)failConnect:(NSString *)reason {
	if (self.closing) {
		return;
	}
	[self bump];
	self.phase = AtvvPhaseIdle;
	[self.central stopScan];
	if (self.peripheral != nil && self.central != nil) {
		[self.central cancelPeripheralConnection:self.peripheral];
	}
	[self emit:ATVV_EVENT_CONNECT_FAILED message:reason];
}

// armTimeout fails the current phase when nothing else bumped the generation
// before the deadline. Every state transition must call bump.
- (void)armTimeout:(NSTimeInterval)seconds message:(NSString *)message {
	NSInteger gen = self.generation;
	__weak typeof(self) weakSelf = self;
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(seconds * NSEC_PER_SEC)), self.queue, ^{
		typeof(self) self = weakSelf;
		if (self == nil || self.closing) {
			return;
		}
		if (self.generation != gen) {
			return;
		}
		if (self.phase == AtvvPhaseReady || self.phase == AtvvPhaseIdle) {
			return;
		}
		[self failConnect:message];
	});
}

- (BOOL)nameMatches:(CBPeripheral *)peripheral {
	if (self.wantName.length == 0) {
		return YES;
	}
	NSString *name = peripheral.name;
	if (name == nil) {
		return YES;
	}
	return [name localizedCaseInsensitiveContainsString:self.wantName];
}

#pragma mark - connect pipeline (queue only)

- (void)startConnectOnQueue {
	if (self.closing) {
		return;
	}
	[self bump];
	switch (self.central.state) {
		case CBManagerStatePoweredOn:
			[self resolveCandidate];
			return;
		case CBManagerStatePoweredOff:
			[self failConnect:@"Bluetooth is powered off"];
			return;
		case CBManagerStateUnauthorized:
			[self failConnect:@"Bluetooth access denied; allow it in System Settings"];
			return;
		case CBManagerStateUnsupported:
			[self failConnect:@"Bluetooth LE unsupported on this machine"];
			return;
		default:
			self.phase = AtvvPhaseResolving;
			[self armTimeout:kStateTimeout message:@"Bluetooth power-on timed out"];
			return;
	}
}

- (void)resolveCandidate {
	self.phase = AtvvPhaseResolving;
	CBPeripheral *candidate = nil;
	if (self.wantID.length > 0) {
		NSUUID *uuid = [[NSUUID alloc] initWithUUIDString:self.wantID];
		if (uuid == nil) {
			[self failConnect:@"invalid peripheral identifier"];
			return;
		}
		NSArray<CBPeripheral *> *found = [self.central retrievePeripheralsWithIdentifiers:@[uuid]];
		if (found.count == 0) {
			[self failConnect:@"known peripheral not found; remove the identifier and retry"];
			return;
		}
		candidate = found.firstObject;
	} else {
		NSMutableArray<CBUUID *> *services = [NSMutableArray arrayWithObjects:
			[CBUUID UUIDWithString:kAtvvServiceUUID],
			[CBUUID UUIDWithString:kHogServiceUUID], nil];
		NSArray<CBPeripheral *> *connected = [self.central retrieveConnectedPeripheralsWithServices:services];
		for (CBPeripheral *item in connected) {
			if ([self nameMatches:item]) {
				candidate = item;
				break;
			}
		}
	}
	if (candidate != nil) {
		[self connectCandidate:candidate];
		return;
	}
	// Not connected (or unknown): the remote advertises ATVV while idle.
	[self.central scanForPeripheralsWithServices:@[[CBUUID UUIDWithString:kAtvvServiceUUID]] options:nil];
	[self armTimeout:kScanTimeout message:@"no ATVV remote found; is it powered and in range?"];
}

- (void)connectCandidate:(CBPeripheral *)peripheral {
	[self bump];
	self.phase = AtvvPhaseConnecting;
	self.peripheral = peripheral;
	peripheral.delegate = self;
	[self.central connectPeripheral:peripheral options:nil];
	[self armTimeout:kConnectTimeout message:@"connecting to the remote timed out"];
}

#pragma mark - CBCentralManagerDelegate

- (void)centralManagerDidUpdateState:(CBCentralManager *)central {
	if (self.closing) {
		return;
	}
	if (central.state == CBManagerStatePoweredOn) {
		if (self.phase == AtvvPhaseResolving) {
			[self resolveCandidate];
		}
		return;
	}
	if (self.phase != AtvvPhaseIdle && self.phase != AtvvPhaseReady) {
		[self failConnect:[NSString stringWithFormat:@"Bluetooth unavailable (state %ld)", (long)central.state]];
	}
}

- (void)centralManager:(CBCentralManager *)central
	didDiscoverPeripheral:(CBPeripheral *)peripheral
	advertisementData:(NSDictionary<NSString *, id> *)advertisementData
	RSSI:(NSNumber *)RSSI
{
	if (self.closing || self.phase != AtvvPhaseResolving || self.peripheral != nil) {
		return;
	}
	if (![self nameMatches:peripheral]) {
		return;
	}
	[self.central stopScan];
	[self connectCandidate:peripheral];
}

- (void)centralManager:(CBCentralManager *)central didConnectPeripheral:(CBPeripheral *)peripheral {
	if (self.closing || peripheral != self.peripheral) {
		return;
	}
	[self bump];
	self.phase = AtvvPhaseDiscovering;
	[peripheral discoverServices:@[[CBUUID UUIDWithString:kAtvvServiceUUID]]];
	[self armTimeout:kDiscoverTimeout message:@"ATVV service discovery timed out"];
}

- (void)centralManager:(CBCentralManager *)central
	didFailToConnectPeripheral:(CBPeripheral *)peripheral
	error:(NSError *)error
{
	if (self.closing || peripheral != self.peripheral) {
		return;
	}
	[self failConnect:error != nil ? error.localizedDescription : @"connection failed"];
}

- (void)centralManager:(CBCentralManager *)central
	didDisconnectPeripheral:(CBPeripheral *)peripheral
	error:(NSError *)error
{
	if (self.closing || peripheral != self.peripheral) {
		return;
	}
	[self bump];
	self.phase = AtvvPhaseIdle;
	[self emit:ATVV_EVENT_DISCONNECTED
		message:error != nil ? error.localizedDescription : nil];
}

#pragma mark - CBPeripheralDelegate

- (void)peripheral:(CBPeripheral *)peripheral didDiscoverServices:(NSError *)error {
	if (self.closing || peripheral != self.peripheral) {
		return;
	}
	if (error != nil) {
		[self failConnect:[NSString stringWithFormat:@"service discovery failed: %@", error.localizedDescription]];
		return;
	}
	self.atvvService = nil;
	for (CBService *service in peripheral.services) {
		if ([service.UUID isEqual:[CBUUID UUIDWithString:kAtvvServiceUUID]]) {
			self.atvvService = service;
			break;
		}
	}
	if (self.atvvService == nil) {
		[self failConnect:@"ATVV service missing on this device"];
		return;
	}
	[self bump];
	[peripheral discoverCharacteristics:@[
		[CBUUID UUIDWithString:kAtvvTXUUID],
		[CBUUID UUIDWithString:kAtvvAudioUUID],
		[CBUUID UUIDWithString:kAtvvControlUUID],
	] forService:self.atvvService];
	[self armTimeout:kDiscoverTimeout message:@"ATVV characteristic discovery timed out"];
}

- (void)peripheral:(CBPeripheral *)peripheral
	didDiscoverCharacteristicsForService:(CBService *)service
	error:(NSError *)error
{
	if (self.closing || peripheral != self.peripheral || service != self.atvvService) {
		return;
	}
	if (error != nil) {
		[self failConnect:[NSString stringWithFormat:@"characteristic discovery failed: %@", error.localizedDescription]];
		return;
	}
	self.txChar = nil;
	self.audioChar = nil;
	self.controlChar = nil;
	for (CBCharacteristic *characteristic in service.characteristics) {
		if ([characteristic.UUID isEqual:[CBUUID UUIDWithString:kAtvvTXUUID]]) {
			self.txChar = characteristic;
		} else if ([characteristic.UUID isEqual:[CBUUID UUIDWithString:kAtvvAudioUUID]]) {
			self.audioChar = characteristic;
		} else if ([characteristic.UUID isEqual:[CBUUID UUIDWithString:kAtvvControlUUID]]) {
			self.controlChar = characteristic;
		}
	}
	if (self.txChar == nil || self.audioChar == nil || self.controlChar == nil) {
		[self failConnect:@"ATVV characteristics missing on this device"];
		return;
	}
	[self bump];
	self.phase = AtvvPhaseReady;
	[self emit:ATVV_EVENT_GATT_READY message:nil];
}

- (void)peripheral:(CBPeripheral *)peripheral
	didUpdateNotificationStateForCharacteristic:(CBCharacteristic *)characteristic
	error:(NSError *)error
{
	if (self.closing || peripheral != self.peripheral) {
		return;
	}
	if (error != nil || !characteristic.isNotifying) {
		[self bump];
		[self emit:ATVV_EVENT_SUBSCRIBE_FAILED
			message:error != nil ? error.localizedDescription : @"notification enable refused"];
		return;
	}
	if (characteristic == self.audioChar) {
		self.audioNotifying = YES;
	} else if (characteristic == self.controlChar) {
		self.controlNotifying = YES;
	}
	if (self.audioNotifying && self.controlNotifying) {
		[self bump];
		[self emit:ATVV_EVENT_SUBSCRIBED message:nil];
	}
}

- (void)peripheral:(CBPeripheral *)peripheral
	didUpdateValueForCharacteristic:(CBCharacteristic *)characteristic
	error:(NSError *)error
{
	if (self.closing || error != nil || peripheral != self.peripheral) {
		return;
	}
	NSData *value = characteristic.value;
	if (value == nil || value.length == 0) {
		return;
	}
	if (characteristic == self.audioChar && self.onAudio != NULL) {
		self.onAudio((const uint8_t *)value.bytes, value.length, self.user);
	} else if (characteristic == self.controlChar && self.onControl != NULL) {
		self.onControl((const uint8_t *)value.bytes, value.length, self.user);
	}
}

#pragma mark - public (queue-confined state)

- (void)startConnect {
	dispatch_async(self.queue, ^{
		[self startConnectOnQueue];
	});
}

- (void)startSubscribe {
	dispatch_async(self.queue, ^{
		if (self.closing) {
			return;
		}
		if (self.phase != AtvvPhaseReady) {
			[self emit:ATVV_EVENT_SUBSCRIBE_FAILED message:@"not connected"];
			return;
		}
		[self bump];
		self.audioNotifying = NO;
		self.controlNotifying = NO;
		[self.peripheral setNotifyValue:YES forCharacteristic:self.audioChar];
		[self.peripheral setNotifyValue:YES forCharacteristic:self.controlChar];
		[self armTimeout:kSubscribeTimeout message:@"enabling notifications timed out"];
	});
}

- (int)writeTX:(const uint8_t *)data length:(size_t)len errbuf:(char *)errbuf errlen:(size_t)errlen {
	__block int rc = 0;
	dispatch_sync(self.queue, ^{
		if (self.closing || self.phase != AtvvPhaseReady ||
			self.peripheral == nil || self.txChar == nil) {
			setErr(errbuf, errlen, "not connected");
			rc = -1;
			return;
		}
		NSData *payload = [NSData dataWithBytes:data length:len];
		CBCharacteristicWriteType type =
			(self.txChar.properties & CBCharacteristicPropertyWriteWithoutResponse) != 0
				? CBCharacteristicWriteWithoutResponse
				: CBCharacteristicWriteWithResponse;
		[self.peripheral writeValue:payload forCharacteristic:self.txChar type:type];
	});
	return rc;
}

- (const char *)identifierCString {
	__block const char *cached = NULL;
	dispatch_sync(self.queue, ^{
		if (self.closing || self.phase != AtvvPhaseReady || self.peripheral == nil) {
			return;
		}
		if (self.identifierC == NULL) {
			self.identifierC = strdup(self.peripheral.identifier.UUIDString.UTF8String);
		}
		cached = self.identifierC;
	});
	return cached;
}

- (void)teardown {
	dispatch_sync(self.queue, ^{
		if (self.closing) {
			return;
		}
		self.closing = YES;
		[self bump];
		[self.central stopScan];
		if (self.peripheral != nil) {
			self.peripheral.delegate = nil;
			[self.central cancelPeripheralConnection:self.peripheral];
		}
		self.central.delegate = nil;
		self.peripheral = nil;
		self.atvvService = nil;
		self.txChar = nil;
		self.audioChar = nil;
		self.controlChar = nil;
		self.phase = AtvvPhaseIdle;
		if (self.identifierC != NULL) {
			free(self.identifierC);
			self.identifierC = NULL;
		}
	});
}

@end

#pragma mark - C ABI

void *atvv_open(const char *id, const char *name_hint,
	atvv_data_cb on_audio, atvv_data_cb on_control, atvv_event_cb on_event,
	void *user, char *errbuf, size_t errlen)
{
	@autoreleasepool {
		NSString *wantID = id != NULL && id[0] != '\0'
			? [NSString stringWithUTF8String:id] : @"";
		NSString *wantName = name_hint != NULL && name_hint[0] != '\0'
			? [NSString stringWithUTF8String:name_hint] : @"";
		AtvvBridge *bridge = [[AtvvBridge alloc]
			initWithID:wantID nameHint:wantName user:user
			onAudio:on_audio onControl:on_control onEvent:on_event];
		if (bridge == nil) {
			setErr(errbuf, errlen, "bridge allocation failed");
			return NULL;
		}
		return (__bridge_retained void *)bridge;
	}
}

static AtvvBridge *bridgeFrom(void *handle) {
	return (__bridge AtvvBridge *)handle;
}

int atvv_start_connect(void *handle, char *errbuf, size_t errlen) {
	if (handle == NULL) {
		setErr(errbuf, errlen, "nil handle");
		return -1;
	}
	@autoreleasepool {
		[bridgeFrom(handle) startConnect];
	}
	return 0;
}

int atvv_start_subscribe(void *handle, char *errbuf, size_t errlen) {
	if (handle == NULL) {
		setErr(errbuf, errlen, "nil handle");
		return -1;
	}
	@autoreleasepool {
		[bridgeFrom(handle) startSubscribe];
	}
	return 0;
}

int atvv_write(void *handle, const uint8_t *data, size_t len,
	char *errbuf, size_t errlen)
{
	if (handle == NULL) {
		setErr(errbuf, errlen, "nil handle");
		return -1;
	}
	@autoreleasepool {
		return [bridgeFrom(handle) writeTX:data length:len errbuf:errbuf errlen:errlen];
	}
}

const char *atvv_identifier(void *handle) {
	if (handle == NULL) {
		return NULL;
	}
	@autoreleasepool {
		return [bridgeFrom(handle) identifierCString];
	}
}

void atvv_close(void *handle) {
	if (handle == NULL) {
		return;
	}
	AtvvBridge *bridge = (__bridge_transfer AtvvBridge *)handle;
	[bridge teardown];
	// ARC releases the bridge here.
}
