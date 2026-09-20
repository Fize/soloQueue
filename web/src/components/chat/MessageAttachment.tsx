import { useState } from 'react'
import { File } from 'lucide-react'
import { getFileUrl } from '@/lib/api'

export function MessageAttachment({ file }: { file: { name: string; path: string } }) {
  const imageExtension = /\.(jpe?g|png|webp|gif|svg|avif|bmp|ico)(?:$|[?#])/i
  const knownImage = imageExtension.test(file.name) || imageExtension.test(file.path)
  // Older QQ attachments are saved as "download", with no MIME type or extension.
  // Let the browser decode those files; non-images keep their ordinary file link.
  const extensionless = !/\.[^./\\]+$/.test(file.path.split(/[?#]/)[0])
  const [imageState, setImageState] = useState<'loading' | 'loaded' | 'failed'>('loading')
  const showImage = (knownImage || imageState === 'loaded') && imageState !== 'failed'
  const probeImage = (knownImage || extensionless) && imageState !== 'failed'

  return (
    <a
      href={getFileUrl(file.path)}
      target="_blank"
      rel="noreferrer"
      className={showImage
        ? 'block max-w-[240px] rounded-lg overflow-hidden border border-border bg-muted p-1 hover:opacity-90 transition-opacity'
        : 'flex items-center gap-2 p-2 rounded-lg border border-border bg-card/50 hover:bg-card text-foreground text-xs max-w-xs transition-colors'}
    >
      {probeImage && (
        <img
          src={getFileUrl(file.path)}
          alt={file.name}
          className={showImage ? 'max-h-[220px] max-w-full object-contain rounded border border-black/10 dark:border-white/10 bg-[repeating-conic-gradient(#e5e7eb_0%_25%,#f9fafb_0%_50%)_50%_/_12px_12px] dark:bg-[repeating-conic-gradient(#374151_0%_25%,#1f2937_0%_50%)_50%_/_12px_12px]' : 'hidden'}
          onLoad={() => setImageState('loaded')}
          onError={() => setImageState('failed')}
        />
      )}
      {!showImage && <><File className="h-4 w-4 shrink-0 opacity-70" /><span className="truncate flex-1 font-medium">{file.name}</span></>}
    </a>
  )
}
