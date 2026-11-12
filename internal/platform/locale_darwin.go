//go:build darwin && !ios

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>

static char *onrdpPreferredLocale(void) {
	@autoreleasepool {
		NSString *locale = [[NSLocale preferredLanguages] firstObject];
		if (locale == nil) return NULL;
		return strdup([locale UTF8String]);
	}
}
*/
import "C"

import "unsafe"

// osLocale reads the first macOS preferred language, which is not necessarily
// present in the process's POSIX locale environment.
func osLocale() string {
	p := C.onrdpPreferredLocale()
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}
