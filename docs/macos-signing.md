# macOS Fixed Self-Signing

SoloQueue uses the fixed application identifier `com.soloqueue` and a fixed self-signed identity named `SoloQueue Code Signing`. This is a controlled-distribution setup, not Apple notarization.

## First-Time Setup

Run:

```bash
make setup-macos-signing
```

The setup opens hidden macOS password dialogs. Choose a password with at least 12 characters and store it in a separate password manager. The script creates:

- An identity in the current user's login Keychain.
- An encrypted private-key backup at `~/Documents/SoloQueue Signing Backup/SoloQueue-Code-Signing.p12`.
- A public certificate and public SHA-256 fingerprint in the same backup directory.

The PKCS#12 file contains the private key. Never commit it, send it with the application, or store its password beside it.

After initial creation, copy the public fingerprint from the backup directory into `desktop/build/macos-signing-cert.sha256` and commit only that fingerprint file. A normal package build fails until the pin exists.

Verify the installed identity:

```bash
make check-macos-signing
```

## Packaging

```bash
make package-desktop PLATFORM=mac
```

Packaging fails if the fixed identity is unavailable, ambiguous, ad-hoc, or different from the pinned certificate. The packaged app, Electron helpers, and bundled Go backend are verified before the artifact is accepted.

## Moving to Another Mac

Do not generate a new certificate. A new certificate changes the macOS code identity and can require privacy permissions again.

1. Copy `SoloQueue-Code-Signing.p12` to the new Mac through a trusted channel.
2. Check out a SoloQueue revision containing the same `desktop/build/macos-signing-cert.sha256` pin.
3. Run the restore command with an absolute path:

   ```bash
   ./scripts/setup-macos-signing-cert.sh --restore "/absolute/path/SoloQueue-Code-Signing.p12"
   ```

4. Enter the original backup password once in the hidden dialog. The restore process keeps the decrypted private key in memory and grants access only to `/usr/bin/codesign`.
5. Run `make check-macos-signing` and then build the macOS package.

The restore script validates the certificate name, code-signing usage, and SHA-256 fingerprint before changing the Keychain.

## Upgrade Verification

For two consecutive builds, compare:

```bash
codesign -d -r- "/path/to/old/SoloQueue.app"
codesign -d -r- "/path/to/new/SoloQueue.app"
codesign -d -r- "/path/to/new/SoloQueue.app/Contents/Resources/soloqueue"
```

The application requirements must keep `com.soloqueue`; the backend requirement must keep `com.soloqueue.backend`; neither requirement may be `cdhash`-only.

Changing from the former `com.xiaobaitu.soloqueue` identifier to `com.soloqueue` requires one final macOS privacy approval. The fixed identity protects continuity for later upgrades; it cannot silently grant TCC permissions.

## Recovery Limits

- Losing either the PKCS#12 backup or its password makes the identity unrecoverable on another Mac.
- Anyone who obtains both can sign code as this SoloQueue identity.
- A compromised identity must be replaced, which intentionally breaks permission continuity.
- Self-signed builds remain non-notarized and may require recipients to approve launch through macOS security controls.
