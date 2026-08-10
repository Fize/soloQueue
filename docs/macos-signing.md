# macOS Fixed Self-Signing (Maintainer Reference)

I use this document when I package a local macOS build. I do not treat it as an
end-user installation guide, and it does not provide Apple Developer ID
signing or notarization.

中文：[macOS 固定自签名](zh/macos-signing.md)

I use the fixed application identifier `com.soloqueue` and a fixed self-signed
identity named `SoloQueue Code Signing`. I treat this as a controlled-
distribution setup, not Apple notarization.

## First-Time Setup

I run:

```bash
make setup-macos-signing
```

I see the setup open hidden macOS password dialogs. I choose a password with at
least 12 characters and store it in a separate password manager. The script
creates:

- I create an identity in the current user's login Keychain.
- I create an encrypted private-key backup at `~/Documents/SoloQueue Signing Backup/SoloQueue-Code-Signing.p12`.
- I create a public certificate and public SHA-256 fingerprint in the same backup directory.

I treat the PKCS#12 file as a private-key container. I never commit it, send it
with the application, or store its password beside it.

After initial creation, I copy the public fingerprint from the backup directory
into `desktop/build/macos-signing-cert.sha256` and commit only that fingerprint
file. I expect a normal package build to fail until the pin exists.

I verify the installed identity:

```bash
make check-macos-signing
```

## Packaging

```bash
make package-desktop PLATFORM=mac
```

I expect packaging to fail if the fixed identity is unavailable, ambiguous,
ad-hoc, or different from the pinned certificate. I verify the packaged app,
Electron helpers, and bundled Go backend before accepting the artifact.

## Moving to Another Mac

I do not generate a new certificate when moving to another Mac. A new
certificate changes the macOS code identity and can require privacy permissions
again.

1. I copy `SoloQueue-Code-Signing.p12` to the new Mac through a trusted channel.
2. I check out a SoloQueue revision containing the same `desktop/build/macos-signing-cert.sha256` pin.
3. I run the restore command with an absolute path:

   ```bash
   ./scripts/setup-macos-signing-cert.sh --restore "/absolute/path/SoloQueue-Code-Signing.p12"
   ```

4. I enter the original backup password once in the hidden dialog. I let the
   restore process keep the decrypted private key in memory and grant access
   only to `/usr/bin/codesign`.
5. I run `make check-macos-signing` and then build the macOS package.

I rely on the restore script to validate the certificate name, code-signing
usage, and SHA-256 fingerprint before changing the Keychain.

## Upgrade Verification

For two consecutive builds, I compare:

```bash
codesign -d -r- "/path/to/old/SoloQueue.app"
codesign -d -r- "/path/to/new/SoloQueue.app"
codesign -d -r- "/path/to/new/SoloQueue.app/Contents/Resources/soloqueue"
```

I keep `com.soloqueue` in the application requirements and
`com.soloqueue.backend` in the backend requirement; neither requirement may be
`cdhash`-only.

When I change from the former `com.xiaobaitu.soloqueue` identifier to
`com.soloqueue`, I need one final macOS privacy approval. I use the fixed
identity to protect continuity for later upgrades; it cannot silently grant TCC
permissions.

## Recovery Limits

- If I lose either the PKCS#12 backup or its password, I cannot recover the
  identity on another Mac.
- Anyone who obtains both can sign code as this SoloQueue identity.
- If the identity is compromised, I replace it and intentionally break
  permission continuity.
- I treat self-signed builds as non-notarized and expect recipients to approve
  launch through macOS security controls when required.
