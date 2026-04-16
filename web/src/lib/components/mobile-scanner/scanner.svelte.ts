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
const CAMERA_KEY = 'preferred_camera_device_id';

export type Scanner = ReturnType<typeof createScanner>;

export function createScanner(getVideoEl: () => HTMLVideoElement | null) {
  let mediaStream: MediaStream | null = null;
  let cameraError = $state<string | null>(null);
  let scanState = $state<ScanState>('idle');
  let cameras = $state<MediaDeviceInfo[]>([]);
  let continuousActive = $state(false);
  let selectedCameraId = $state<string | null>(localStorage.getItem(CAMERA_KEY));

  function selectCamera(deviceId: string) {
    selectedCameraId = deviceId;
    localStorage.setItem(CAMERA_KEY, deviceId);
  }

  async function startCamera(deviceId?: string | null) {
    const el = getVideoEl();
    if (!el || mediaStream) return;
    cameraError = null;
    // Preload WASM in parallel while waiting for camera permission.
    prepareZXingModule({ overrides: { locateFile: () => zxingWasmUrl } });
    try {
      const constraints = deviceId
        ? { video: { deviceId: { exact: deviceId } } }
        : { video: { facingMode: { ideal: 'environment' } } };
      const stream = await navigator.mediaDevices.getUserMedia(constraints);
      mediaStream = stream;
      el.srcObject = stream;
      // Enumerate cameras now that permission is granted (labels become available).
      const devices = await navigator.mediaDevices.enumerateDevices();
      cameras = devices.filter(d => d.kind === 'videoinput');
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
    continuousActive = false;
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

  async function decodeFrame(imageData: ImageData): Promise<string | null> {
    try {
      const results = await readBarcodes(imageData, {
        tryHarder: true,
        maxNumberOfSymbols: 1,
        textMode: 'Plain',
        formats: ["DataMatrix", "QRCode"]
      });
      const valid = results.filter(r => r.isValid);
      return valid.length > 0 ? valid[0].text : null;
    } catch {
      return null;
    }
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

    const text = await decodeFrame(imageData);
    if (!text) {
      scanState = 'no_barcode';
      setTimeout(() => { if (scanState === 'no_barcode') scanState = 'idle'; }, 1500);
      return;
    }

    try {
      await onResult(text);
    } finally {
      scanState = 'idle';
    }
  }

  /**
   * Continuously decodes frames until a barcode is found or stopContinuousScan() is called.
   * Designed for hold-to-scan: call on pointerdown, call stopContinuousScan on pointerup/cancel.
   */
  async function startContinuousScan(onResult: (text: string) => Promise<void>): Promise<void> {
    if (continuousActive) return;
    continuousActive = true;
    scanState = 'decoding';

    while (continuousActive) {
      // Yield a macrotask between each decode so the event loop can process
      // pointerup/pointercancel before the next blocking WASM call.
      await new Promise(r => setTimeout(r, 0));
      if (!continuousActive) break;

      const imageData = captureFrame();
      if (!imageData) continue;

      const text = await decodeFrame(imageData);
      if (text && continuousActive) {
        continuousActive = false;
        scanState = 'idle';
        await onResult(text);
        return;
      }
    }

    scanState = 'idle';
  }

  function stopContinuousScan() {
    continuousActive = false;
  }

  return {
    get cameraError() { return cameraError; },
    get scanState() { return scanState; },
    get cameras() { return cameras; },
    get isScanning() { return continuousActive; },
    get selectedCameraId() { return selectedCameraId; },
    startCamera,
    stopCamera,
    selectCamera,
    triggerScan,
    startContinuousScan,
    stopContinuousScan,
  };
}
