const { spawnSync } = require('child_process')
const path = require('path')
const fs = require('fs')

const SIGNING_IDENTITY = 'SoloQueue Code Signing'
const APP_IDENTIFIER = 'com.soloqueue'
const BACKEND_IDENTIFIER = 'com.soloqueue.backend'

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    encoding: 'utf8',
    ...options
  })

  if (result.error) throw result.error
  if (result.status !== 0) {
    const detail = (result.stderr || result.stdout || '').trim()
    throw new Error(`${path.basename(command)} failed: ${detail || `exit ${result.status}`}`)
  }

  return `${result.stdout || ''}${result.stderr || ''}`.trim()
}

function normalizeFingerprint(value) {
  return value.replaceAll(':', '').trim().toLowerCase()
}

function readPinnedFingerprint() {
  const pinPath = path.join(__dirname, '..', 'macos-signing-cert.sha256')
  if (!fs.existsSync(pinPath)) {
    throw new Error(`Pinned signing certificate fingerprint not found: ${pinPath}`)
  }

  const fingerprint = normalizeFingerprint(fs.readFileSync(pinPath, 'utf8'))
  if (!/^[0-9a-f]{64}$/.test(fingerprint)) {
    throw new Error(`Invalid pinned signing certificate fingerprint: ${pinPath}`)
  }
  return fingerprint
}

function verifyKeychainIdentity(expectedFingerprint) {
  const identities = run('/usr/bin/security', ['find-identity', '-v', '-p', 'codesigning'])
  const matches = identities.split('\n').filter((line) => {
    const match = line.match(/"([^"]+)"/)
    return match?.[1] === SIGNING_IDENTITY
  })

  if (matches.length !== 1) {
    throw new Error(
      `Expected exactly one valid "${SIGNING_IDENTITY}" identity, found ${matches.length}`
    )
  }

  const certificate = run('/usr/bin/security', [
    'find-certificate',
    '-c',
    SIGNING_IDENTITY,
    '-p'
  ])
  const fingerprintOutput = run(
    '/usr/bin/openssl',
    ['x509', '-noout', '-fingerprint', '-sha256'],
    { input: certificate }
  )
  const fingerprint = normalizeFingerprint(fingerprintOutput.split('=').at(-1) || '')

  if (fingerprint !== expectedFingerprint) {
    throw new Error(
      `Signing certificate fingerprint mismatch: expected ${expectedFingerprint}, got ${fingerprint}`
    )
  }
  const sha1Output = run(
    '/usr/bin/openssl',
    ['x509', '-noout', '-fingerprint', '-sha1'],
    { input: certificate }
  )
  const sha1 = normalizeFingerprint(sha1Output.split('=').at(-1) || '')
  if (!/^[0-9a-f]{40}$/.test(sha1)) {
    throw new Error(`Invalid SHA-1 fingerprint resolved for ${SIGNING_IDENTITY}`)
  }
  return sha1
}

function verifySignedTarget(targetPath, expectedIdentifier, expectedCertificateSha1) {
  const info = run('/usr/bin/codesign', ['-dvvv', targetPath])
  if (info.includes('Signature=adhoc') || /flags=.*\badhoc\b/.test(info)) {
    throw new Error(`Ad-hoc signature rejected: ${targetPath}`)
  }
  if (!info.includes(`Identifier=${expectedIdentifier}`)) {
    throw new Error(`Expected identifier ${expectedIdentifier}: ${targetPath}`)
  }

  run('/usr/bin/codesign', [
    '--verify',
    '--strict',
    `-R=identifier "${expectedIdentifier}" and certificate root = H"${expectedCertificateSha1}"`,
    targetPath
  ])

  const requirement = run('/usr/bin/codesign', ['-d', '-r-', targetPath])
  if (requirement.includes('designated => cdhash')) {
    throw new Error(`cdhash-only designated requirement rejected: ${targetPath}`)
  }
  if (!requirement.includes(`identifier "${expectedIdentifier}"`)) {
    throw new Error(`Unstable designated requirement for ${targetPath}: ${requirement}`)
  }
  if (!requirement.toLowerCase().includes(`certificate root = h"${expectedCertificateSha1}"`)) {
    throw new Error(`Pinned certificate requirement not found: ${targetPath}`)
  }
  if (!requirement.includes('anchor') && !requirement.includes('certificate')) {
    throw new Error(`Certificate-based designated requirement not found: ${targetPath}`)
  }
}

exports.default = async function afterSign(context) {
  const { electronPlatformName, appOutDir, packager } = context
  if (electronPlatformName !== 'darwin') return
  const startedAt = process.hrtime.bigint()

  const appName = packager.appInfo.productFilename
  const appPath = path.join(appOutDir, `${appName}.app`)
  const backendPath = path.join(appPath, 'Contents', 'Resources', 'soloqueue')

  if (!fs.existsSync(appPath)) {
    throw new Error(`Signed application not found: ${appPath}`)
  }
  if (!fs.existsSync(backendPath)) {
    throw new Error(`Signed backend not found: ${backendPath}`)
  }

  const expectedFingerprint = readPinnedFingerprint()
  const identityStartedAt = process.hrtime.bigint()
  const expectedCertificateSha1 = verifyKeychainIdentity(expectedFingerprint)
  const identitySeconds = Number(process.hrtime.bigint() - identityStartedAt) / 1e9
  console.log(`[afterSign] Verified Keychain identity in ${identitySeconds.toFixed(2)}s`)

  // electron-builder already performs a deep bundle verification before this hook.
  const requirementsStartedAt = process.hrtime.bigint()
  verifySignedTarget(appPath, APP_IDENTIFIER, expectedCertificateSha1)
  verifySignedTarget(backendPath, BACKEND_IDENTIFIER, expectedCertificateSha1)
  const requirementsSeconds = Number(process.hrtime.bigint() - requirementsStartedAt) / 1e9
  console.log(`[afterSign] Verified fixed requirements in ${requirementsSeconds.toFixed(2)}s`)

  const elapsedSeconds = Number(process.hrtime.bigint() - startedAt) / 1e9
  console.log(
    `[afterSign] Verified fixed identity ${SIGNING_IDENTITY} (${expectedFingerprint}) in ${elapsedSeconds.toFixed(2)}s`
  )
}
