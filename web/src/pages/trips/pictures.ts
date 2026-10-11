// Pictures for the trip chat are scaled down in the browser before they are
// sent: phone photos are several megabytes, and the assistant only needs to
// read what's on them.

export const MAX_CHAT_PICTURES = 4
const MAX_SIDE = 1600
const JPEG_QUALITY = 0.85

function readAsDataURL(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error ?? new Error('could not read the picture'))
    reader.readAsDataURL(file)
  })
}

/** A picture as a data URL, scaled so its longer side is at most 1600 px. */
export async function pictureToDataURL(file: File): Promise<string> {
  const original = await readAsDataURL(file)
  try {
    const img = new Image()
    await new Promise<void>((resolve, reject) => {
      img.onload = () => resolve()
      img.onerror = () => reject(new Error('not a picture'))
      img.src = original
    })
    const scale = Math.min(1, MAX_SIDE / Math.max(img.naturalWidth, img.naturalHeight))
    // Small pictures that aren't huge files go as they are.
    if (scale === 1 && file.size < 700_000) return original
    const canvas = document.createElement('canvas')
    canvas.width = Math.round(img.naturalWidth * scale)
    canvas.height = Math.round(img.naturalHeight * scale)
    const ctx = canvas.getContext('2d')
    if (!ctx) return original
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height)
    const scaled = canvas.toDataURL('image/jpeg', JPEG_QUALITY)
    return scaled.startsWith('data:image/jpeg') ? scaled : original
  } catch {
    // No canvas or an unusual format: send the file as it is and let the server decide.
    return original
  }
}
