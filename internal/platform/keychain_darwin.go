//go:build darwin && cgo

package platform

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>
static int kc_read(const char* service, void** out, long* n) {
 CFStringRef svc=CFStringCreateWithCString(NULL,service,kCFStringEncodingUTF8);
 const void* keys[]={kSecClass,kSecAttrService,kSecAttrAccount,kSecReturnData,kSecMatchLimit};
 const void* vals[]={kSecClassGenericPassword,svc,CFSTR("local-user"),kCFBooleanTrue,kSecMatchLimitOne};
 CFDictionaryRef q=CFDictionaryCreate(NULL,keys,vals,5,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFTypeRef result=NULL; OSStatus status=SecItemCopyMatching(q,&result);
 if(status==errSecSuccess){CFDataRef d=(CFDataRef)result;*n=CFDataGetLength(d);*out=malloc(*n);memcpy(*out,CFDataGetBytePtr(d),*n);CFRelease(result);}
 CFRelease(q);CFRelease(svc);return (int)status;
}
static int kc_write(const char* service,const void* bytes,long n) {
 CFStringRef svc=CFStringCreateWithCString(NULL,service,kCFStringEncodingUTF8);
 const void* keys[]={kSecClass,kSecAttrService,kSecAttrAccount};const void* vals[]={kSecClassGenericPassword,svc,CFSTR("local-user")};
 CFDictionaryRef q=CFDictionaryCreate(NULL,keys,vals,3,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFDataRef d=CFDataCreate(NULL,bytes,n); const void* uk[]={kSecValueData};const void* uv[]={d};CFDictionaryRef u=CFDictionaryCreate(NULL,uk,uv,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 OSStatus status=SecItemUpdate(q,u);
 if(status==errSecItemNotFound){CFMutableDictionaryRef add=CFDictionaryCreateMutableCopy(NULL,0,q);CFDictionarySetValue(add,kSecValueData,d);CFDictionarySetValue(add,kSecAttrAccessible,kSecAttrAccessibleWhenUnlockedThisDeviceOnly);status=SecItemAdd(add,NULL);CFRelease(add);}
 CFRelease(q);CFRelease(u);CFRelease(d);CFRelease(svc);return (int)status;
}
*/
import "C"
import (
	"context"
	"encoding/json"
	"github.com/bright-k/tossinvest-tools/internal/broker/toss"
	"github.com/bright-k/tossinvest-tools/internal/fault"
	"unsafe"
)

const TossService = "com.bright.tossinvest-tools.toss-client"
const MCPService = "com.bright.tossinvest-tools.mcp.codex-readonly"

func keyRead(service string) ([]byte, error) {
	s := C.CString(service)
	defer C.free(unsafe.Pointer(s))
	var p unsafe.Pointer
	var n C.long
	status := C.kc_read(s, &p, &n)
	if status != 0 {
		return nil, fault.New("NOT_READY", "Keychain item missing, locked, or access denied")
	}
	defer C.free(p)
	return C.GoBytes(p, C.int(n)), nil
}
func keyWrite(service string, b []byte) error {
	s := C.CString(service)
	defer C.free(unsafe.Pointer(s))
	p := C.CBytes(b)
	defer C.free(p)
	if C.kc_write(s, p, C.long(len(b))) != 0 {
		return fault.New("NOT_READY", "Keychain save denied or locked")
	}
	return nil
}

type TossSecrets struct{}

func (TossSecrets) Read(context.Context) (toss.Credentials, error) {
	var c toss.Credentials
	b, e := keyRead(TossService)
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func SaveToss(c toss.Credentials) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return keyWrite(TossService, b)
}
func ReadMCP() (string, error)   { b, e := keyRead(MCPService); return string(b), e }
func SaveMCP(token string) error { return keyWrite(MCPService, []byte(token)) }
