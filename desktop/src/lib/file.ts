// Centralized file type, extension checks, and binary detection utilities

export const codeExts = new Set([
  '.ts', '.tsx', '.js', '.jsx', '.mjs', '.cjs', '.py', '.pyi', '.pyx',
  '.rs', '.go', '.c', '.cpp', '.cc', '.cxx', '.h', '.hpp', '.hxx',
  '.java', '.kt', '.kts', '.scala', '.json', '.yaml', '.yml', '.toml',
  '.css', '.scss', '.less', '.html', '.htm', '.xml', '.svg',
  '.sh', '.bash', '.zsh', '.fish', '.sql', '.psql',
  '.dockerfile', '.proto', '.graphql', '.gql',
  '.lua', '.rb', '.php', '.swift', '.r', '.dart',
  '.tf', '.hcl', '.vue', '.svelte', '.ini', '.cfg',
])

export const markdownExts = new Set(['.md', '.markdown'])

export const mediaExts = new Set([
  // images
  '.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.ico', '.svg',
  // audio
  '.mp3', '.wav', '.ogg', '.flac', '.aac', '.m4a', '.opus',
  // video
  '.mp4', '.webm', '.mov', '.avi', '.mkv',
])

export const plainExts = new Set([
  '.txt', '.log', '.mod', '.sum', '.Makefile',
  '.dockerignore', '.gitignore', '.env', '.envrc',
])

export const allPreviewableExts = new Set([
  ...codeExts,
  ...markdownExts,
  ...mediaExts,
  ...plainExts,
])

export const extToLanguage: Record<string, string> = {
  '.ts': 'typescript',
  '.tsx': 'tsx',
  '.js': 'javascript',
  '.jsx': 'jsx',
  '.mjs': 'javascript',
  '.cjs': 'javascript',
  '.py': 'python',
  '.pyi': 'python',
  '.rs': 'rust',
  '.go': 'go',
  '.c': 'c',
  '.cpp': 'cpp',
  '.cc': 'cpp',
  '.cxx': 'cpp',
  '.h': 'c',
  '.hpp': 'cpp',
  '.hxx': 'cpp',
  '.java': 'java',
  '.kt': 'java',
  '.json': 'json',
  '.yaml': 'yaml',
  '.yml': 'yaml',
  '.css': 'css',
  '.scss': 'css',
  '.html': 'markup',
  '.htm': 'markup',
  '.xml': 'markup',
  '.svg': 'markup',
  '.sh': 'bash',
  '.bash': 'bash',
  '.zsh': 'bash',
  '.sql': 'sql',
  '.toml': 'toml',
  '.dockerfile': 'docker',
  '.graphql': 'graphql',
  '.gql': 'graphql',
  '.proto': 'protobuf',
}

export const imageExtensions = ['.png', '.jpg', '.jpeg', '.gif', '.svg', '.webp', '.bmp', '.ico']
export const audioExtensions = ['.mp3', '.wav', '.ogg', '.flac', '.aac', '.m4a', '.opus']
export const videoExtensions = ['.mp4', '.webm', '.mov', '.avi', '.mkv']

export const plainTextExtensions = new Set([
  '.markdown', '.txt', '.log', '.mod', '.sum', '.pyx',
  '.kts', '.scala', '.ini', '.cfg', '.less', '.fish', '.psql',
  '.Makefile', '.dockerignore', '.gitignore', '.env', '.envrc',
  '.lua', '.rb', '.php', '.swift', '.r', '.dart',
  '.tf', '.hcl', '.vue', '.svelte',
])

// Well-known text filenames that don't have extensions.
export const knownTextFilenames = new Set([
  'Dockerfile', 'Makefile', 'README', 'LICENSE', 'CHANGELOG',
  'Procfile', 'Jenkinsfile', 'Vagrantfile',
])

export function getExt(path: string): string {
  const name = path.split('/').pop() ?? path
  const dot = name.lastIndexOf('.')
  if (dot === -1) return ''
  return name.slice(dot).toLowerCase()
}

export function isPreviewableExt(ext: string, name?: string): boolean {
  if (!ext) {
    if (name && knownTextFilenames.has(name)) return true
    return false
  }
  return allPreviewableExts.has(ext.toLowerCase())
}

export function isBinaryFile(path: string): boolean {
  const ext = getExt(path)
  if (!ext) {
    const name = path.split('/').pop() ?? path
    return !knownTextFilenames.has(name)
  }
  if (extToLanguage[ext]) return false
  if (ext === '.md' || ext === '.markdown') return false
  if (imageExtensions.includes(ext)) return false
  if (audioExtensions.includes(ext)) return false
  if (videoExtensions.includes(ext)) return false
  if (plainTextExtensions.has(ext)) return false
  return true
}

// looksBinary checks whether the first N bytes contain a NUL byte (0x00),
// matching the backend's binary detection heuristic.
export function looksBinary(buf: ArrayBuffer): boolean {
  const n = Math.min(buf.byteLength, 512)
  const view = new Uint8Array(buf, 0, n)
  for (let i = 0; i < n; i++) {
    if (view[i] === 0) return true
  }
  return false
}
