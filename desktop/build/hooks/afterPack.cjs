const { execFileSync } = require('child_process')
const fs = require('fs')
const path = require('path')

const SIGNING_IDENTITY = 'SoloQueue Code Signing'
const BACKEND_IDENTIFIER = 'com.soloqueue.backend'

exports.default = async function afterPack(context) {
  const { electronPlatformName, appOutDir, packager } = context
  if (electronPlatformName !== 'darwin') return

  const appName = packager.appInfo.productFilename
  const backendPath = path.join(
    appOutDir,
    `${appName}.app`,
    'Contents',
    'Resources',
    'soloqueue'
  )

  if (!fs.existsSync(backendPath)) {
    throw new Error(`Bundled backend not found: ${backendPath}`)
  }

  // The backend keeps a fixed identifier because macOS can track its code identity separately.
  execFileSync(
    '/usr/bin/codesign',
    [
      '--force',
      '--sign',
      SIGNING_IDENTITY,
      '--identifier',
      BACKEND_IDENTIFIER,
      backendPath
    ],
    { stdio: 'inherit' }
  )

  execFileSync(
    '/usr/bin/codesign',
    [
      '--verify',
      '--strict',
      `-R=certificate leaf[subject.CN] = "${SIGNING_IDENTITY}"`,
      backendPath
    ],
    { stdio: 'inherit' }
  )

  console.log(`[afterPack] Signed backend as ${BACKEND_IDENTIFIER}`)
}
