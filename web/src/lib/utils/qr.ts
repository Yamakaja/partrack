/** Shared helpers for QR code copy and download actions. */

const PNG_SIZE = 1024; // pixels; enough for crisp label printing

/** Converts an SVG string to a PNG Blob via an offscreen canvas. */
async function svgToPngBlob(svg: string): Promise<Blob> {
  const blob = new Blob([svg], { type: 'image/svg+xml' });
  const url = URL.createObjectURL(blob);
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const el = new Image();
      el.onload = () => resolve(el);
      el.onerror = reject;
      el.src = url;
    });
    const canvas = document.createElement('canvas');
    canvas.width = PNG_SIZE;
    canvas.height = PNG_SIZE;
    const ctx = canvas.getContext('2d')!;
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, PNG_SIZE, PNG_SIZE);
    ctx.drawImage(img, 0, 0, PNG_SIZE, PNG_SIZE);
    return await new Promise<Blob>((resolve, reject) =>
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('toBlob failed'))), 'image/png'),
    );
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** Copies the QR code as a PNG image to the system clipboard. */
export async function copyQRImage(svg: string): Promise<void> {
  const blob = await svgToPngBlob(svg);
  await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]);
}

/** Downloads the QR code as a PNG file. */
export async function downloadQRPng(svg: string, filename: string): Promise<void> {
  const blob = await svgToPngBlob(svg);
  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
  } finally {
    URL.revokeObjectURL(url);
  }
}

/** Copies a plain-text string to the system clipboard. */
export async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text);
}
