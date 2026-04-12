import { readBarcodes, prepareZXingModule } from 'zxing-wasm/reader';
import zxingWasmUrl from 'zxing-wasm/reader/zxing_reader.wasm?url';

type ScanState = 'idle' | 'decoding' | 'no_barcode';

/**
 * Shared camera + barcode scanning logic for mobile scanner screens.
 *
 * Usage:
 *   let videoEl = $state<HTMLVideoElement | null>(null);
 *   const scanner = createScanner(() => videoEl);
 *
 *   $effect(() => {
 *     if (isScanning && videoEl) {
 *       scanner.startCamera();
 *       return () => scanner.stopCamera();
 *     }
 *   });
 */
export function createScanner(getVideoEl: () => HTMLVideoElement | null) {
  let mediaStream: MediaStream | null = null;
  let cameraError = $state<string | null>(null);
  let scanState = $state<ScanState>('idle');

  async function startCamera() {
    const el = getVideoEl();
    if (!el || mediaStream) return;
    cameraError = null;
    // Preload WASM in parallel while waiting for camera permission.
    prepareZXingModule({ overrides: { locateFile: () => zxingWasmUrl } });
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: 'environment' } },
      });
      mediaStream = stream;
      el.srcObject = stream;
    } catch (e: unknown) {
      const name = (e instanceof Error) ? e.name : '';
      if (name === 'NotAllowedError' || name === 'PermissionDeniedError') {
        cameraError = 'Camera permission denied. Allow camera access in your browser settings and reload.';
      } else if (name === 'SecurityError' || (window.location.protocol !== 'https:' && window.location.hostname !== 'localhost')) {
        cameraError = 'Camera requires a secure connection (HTTPS). Open this page over HTTPS.';
      } else if (name === 'NotFoundError' || name === 'DevicesNotFoundError') {
        cameraError = 'No camera found on this device.';
      } else {
        cameraError = 'Could not start camera. Check permissions and try again.';
      }
    }
  }

  function stopCamera() {
    mediaStream?.getTracks().forEach(t => t.stop());
    mediaStream = null;
    const el = getVideoEl();
    if (el) el.srcObject = null;
    cameraError = null;
  }

  function captureFrame(): ImageData | null {
    const el = getVideoEl();
    if (!el || el.readyState < 2) return null;
    const canvas = new OffscreenCanvas(el.videoWidth, el.videoHeight);
    const ctx = canvas.getContext('2d')!;
    ctx.drawImage(el, 0, 0);
    return ctx.getImageData(0, 0, canvas.width, canvas.height);
  }

  /**
   * Captures a single frame and attempts to decode a barcode.
   * Calls onResult(text) when a barcode is found; manages scanState throughout.
   * Any error thrown by onResult propagates to the caller after resetting scanState.
   */
  async function triggerScan(onResult: (text: string) => Promise<void>): Promise<void> {
    if (scanState === 'decoding') return;
    scanState = 'decoding';

    const imageData = captureFrame();
    if (!imageData) { scanState = 'idle'; return; }

    let text: string;
    try {
      const results = await readBarcodes(imageData, {
        tryHarder: true,
        maxNumberOfSymbols: 1,
        textMode: 'Plain',
        formats: ["DataMatrix", "QRCode"]
      });
      const valid = results.filter(r => r.isValid);
      if (valid.length === 0) {
        scanState = 'no_barcode';
        setTimeout(() => { if (scanState === 'no_barcode') scanState = 'idle'; }, 1500);
        return;
      }
      text = valid[0].text;
    } catch {
      scanState = 'idle';
      return;
    }

    try {
      await onResult(text);
    } finally {
      scanState = 'idle';
    }
  }

  return {
    get cameraError() { return cameraError; },
    get scanState() { return scanState; },
    startCamera,
    stopCamera,
    triggerScan,
  };
}
