#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdio.h>
#include <stdlib.h>

#define MAX_PRIVATE_KEY_SIZE (1024 * 1024)

static void fail(const char *message, OSStatus status) {
    if (status == errSecSuccess) {
        fprintf(stderr, "ERROR: %s\n", message);
    } else {
        fprintf(stderr, "ERROR: %s (%d)\n", message, (int)status);
    }
    exit(1);
}

static void secure_zero(void *memory, size_t size) {
    volatile unsigned char *cursor = memory;
    while (size-- > 0) {
        *cursor++ = 0;
    }
}

static CFMutableDataRef create_private_key_data_from_standard_input(void) {
    CFMutableDataRef data = CFDataCreateMutable(kCFAllocatorDefault, 0);
    if (data == NULL) {
        fail("unable to allocate private-key buffer", errSecSuccess);
    }

    UInt8 buffer[4096];
    size_t total = 0;
    for (;;) {
        size_t count = fread(buffer, 1, sizeof(buffer), stdin);
        if (count > 0) {
            total += count;
            if (total > MAX_PRIVATE_KEY_SIZE) {
                secure_zero(buffer, sizeof(buffer));
                fail("private-key input is too large", errSecSuccess);
            }
            CFDataAppendBytes(data, buffer, (CFIndex)count);
        }
        if (count < sizeof(buffer)) {
            if (ferror(stdin)) {
                secure_zero(buffer, sizeof(buffer));
                fail("unable to read private-key input", errSecSuccess);
            }
            break;
        }
    }
    secure_zero(buffer, sizeof(buffer));
    if (total == 0) {
        fail("private-key input is empty", errSecSuccess);
    }
    return data;
}

static void erase_private_key_data(CFMutableDataRef data) {
    secure_zero(CFDataGetMutableBytePtr(data), (size_t)CFDataGetLength(data));
}

int main(int argc, char **argv) {
    if (argc != 2) {
        fail("expected Keychain path", errSecSuccess);
    }

    CFMutableDataRef private_key_data = create_private_key_data_from_standard_input();
    // The legacy Keychain API is required to target the user's file-backed login Keychain and
    // assign a codesign-only ACL. The modern SecItem API does not expose equivalent ACL control.
    SecKeychainRef keychain = NULL;
    OSStatus status = SecKeychainOpen(argv[1], &keychain);
    if (status != errSecSuccess || keychain == NULL) {
        erase_private_key_data(private_key_data);
        fail("unable to open target Keychain", status);
    }

    SecTrustedApplicationRef codesign_application = NULL;
    status = SecTrustedApplicationCreateFromPath("/usr/bin/codesign", &codesign_application);
    if (status != errSecSuccess || codesign_application == NULL) {
        erase_private_key_data(private_key_data);
        fail("unable to create codesign access entry", status);
    }

    const void *trusted_values[] = {codesign_application};
    CFArrayRef trusted_applications = CFArrayCreate(
        kCFAllocatorDefault,
        trusted_values,
        1,
        &kCFTypeArrayCallBacks
    );
    if (trusted_applications == NULL) {
        erase_private_key_data(private_key_data);
        fail("unable to create trusted application list", errSecSuccess);
    }

    SecAccessRef access = NULL;
    status = SecAccessCreate(CFSTR("SoloQueue Code Signing"), trusted_applications, &access);
    if (status != errSecSuccess || access == NULL) {
        erase_private_key_data(private_key_data);
        fail("unable to create key access control", status);
    }

    SecItemImportExportKeyParameters parameters = {
        .version = SEC_KEY_IMPORT_EXPORT_PARAMS_VERSION,
        .flags = 0,
        .passphrase = NULL,
        .alertTitle = NULL,
        .alertPrompt = NULL,
        .accessRef = access,
        .keyUsage = NULL,
        .keyAttributes = NULL
    };
    SecExternalFormat format = kSecFormatOpenSSL;
    SecExternalItemType item_type = kSecItemTypePrivateKey;
    CFArrayRef imported_items = NULL;
    status = SecItemImport(
        private_key_data,
        CFSTR(".pem"),
        &format,
        &item_type,
        0,
        &parameters,
        keychain,
        &imported_items
    );
    erase_private_key_data(private_key_data);
    if (status != errSecSuccess) {
        fail("private-key import failed", status);
    }
    if (imported_items == NULL || CFArrayGetCount(imported_items) != 1) {
        fail("expected one imported private key", errSecSuccess);
    }

    printf("Imported one SoloQueue private key\n");
    CFRelease(imported_items);
    CFRelease(access);
    CFRelease(trusted_applications);
    CFRelease(codesign_application);
    CFRelease(keychain);
    CFRelease(private_key_data);
    return 0;
}
