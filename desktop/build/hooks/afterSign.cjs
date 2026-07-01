const { execFileSync } = require('child_process')
const path = require('path')
const fs = require('fs')

exports.default = async function afterSign(context) {
  const { electronPlatformName, appOutDir, packager } = context
  if (electronPlatformName !== 'darwin') return

  const appName = packager.appInfo.productFilename
  const appPath = path.join(appOutDir, `${appName}.app`)
  const entitlements = path.join(__dirname, '..', 'entitlements.mac.plist')

  if (!fs.existsSync(appPath)) {
    console.warn(`[afterSign] App not found: ${appPath}, skipping ad-hoc signing`)
    return
  }

  console.log(`[afterSign] Running deep ad-hoc signing on ${appPath}...`)
  // --deep signs all nested binaries recursively
  // --sign - runs ad-hoc signing
  // --force overwrites any existing signature
  execFileSync(
    'codesign',
    [
      '--force',
      '--deep',
      '--sign',
      '-',
      '--entitlements',
      entitlements,
      appPath
    ],
    { stdio: 'inherit' }
  )

  // Verify code signature is valid and self-consistent
  console.log('[afterSign] Verifying code signature...')
  execFileSync('codesign', ['--verify', '--deep', '--strict', '--verbose=2', appPath], {
    stdio: 'inherit'
  })

  console.log('[afterSign] Ad-hoc signing complete')
}
