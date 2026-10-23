// Scan feedback for the phone scanner: one call per scan outcome plays a
// distinct tone, a vibration pattern, and a full-screen flash (ScanFlash.svelte),
// so a scan registers even when the screen's text barely changes — a second
// "match" card replacing an identical first one is otherwise invisible.
//
// Two moments get a cue:
//   scanDecoded()       — the camera read a code (before the server answers):
//                         a short tick on the reticle + a light haptic, so the
//                         user knows to stop aiming.
//   scanFeedback(kind)  — the verdict: tone + vibration + flash per outcome.
//
// Sound needs an AudioContext unlocked by a user gesture; installAudioUnlock()
// resumes it on the first pointerdown/keydown. Vibration uses the Vibration API
// (Android); iOS Safari has none, so we fall back to the iOS 18 switch-input
// haptic, which is best-effort and silently does nothing elsewhere.

export type ScanOutcome = 'success' | 'complete' | 'notice' | 'warning' | 'error';

// ── Preferences (per device, convenience only) ──────────────────────────────

const SOUND_KEY = 'partrack-scan-sound';

function loadSound(): boolean {
  try {
    return localStorage.getItem(SOUND_KEY) !== 'off';
  } catch {
    return true;
  }
}

let soundOn = $state(loadSound());

export const scanFeedbackPrefs = {
  get sound() { return soundOn; },
  set sound(v: boolean) {
    soundOn = v;
    try { localStorage.setItem(SOUND_KEY, v ? 'on' : 'off'); } catch { /* storage blocked */ }
    if (v) scanFeedback('notice'); // audible confirmation the toggle took
  },
};

// ── Pulse state read by ScanFlash / result cards ────────────────────────────

let pulseSeq = $state(0);
let pulseOutcome = $state<ScanOutcome | null>(null);
let decodeSeq = $state(0);

/** Reactive view of the last feedback. `seq` changes on every call, so keying
 *  a result card on it re-animates the card even when its content is identical. */
export const scanPulse = {
  get seq() { return pulseSeq; },
  get outcome() { return pulseOutcome; },
  get decodeSeq() { return decodeSeq; },
};

// ── Audio ───────────────────────────────────────────────────────────────────

let ctx: AudioContext | null = null;

function audio(): AudioContext | null {
  if (ctx) return ctx;
  const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
  if (!Ctor) return null;
  try {
    ctx = new Ctor();
  } catch {
    return null;
  }
  return ctx;
}

let unlockInstalled = false;

/** Resume the AudioContext on the first user gesture (browsers block audio
 *  until then). Idempotent; call once when the scanner mounts. */
export function installAudioUnlock(): void {
  if (unlockInstalled) return;
  unlockInstalled = true;
  const unlock = () => {
    const ac = audio();
    if (ac && ac.state !== 'running') ac.resume().catch(() => {});
  };
  window.addEventListener('pointerdown', unlock, { capture: true, passive: true });
  window.addEventListener('keydown', unlock, { capture: true });
}

type Note = { freq: number; at: number; dur: number; type?: OscillatorType; gain?: number };

function play(notes: Note[]) {
  if (!soundOn) return;
  const ac = audio();
  if (!ac || ac.state !== 'running') return;
  const t0 = ac.currentTime + 0.01;
  for (const n of notes) {
    const osc = ac.createOscillator();
    const g = ac.createGain();
    osc.type = n.type ?? 'sine';
    osc.frequency.value = n.freq;
    const start = t0 + n.at;
    const peak = n.gain ?? 0.25;
    // Short attack/release envelope — a bare start/stop clicks audibly.
    g.gain.setValueAtTime(0, start);
    g.gain.linearRampToValueAtTime(peak, start + 0.008);
    g.gain.setValueAtTime(peak, start + n.dur - 0.02);
    g.gain.linearRampToValueAtTime(0, start + n.dur);
    osc.connect(g).connect(ac.destination);
    osc.start(start);
    osc.stop(start + n.dur + 0.01);
  }
}

// Each outcome is recognisable without looking: rising = good, falling/low
// buzz = bad, a single soft blip = "nothing new".
const TONES: Record<ScanOutcome, Note[]> = {
  success: [
    { freq: 1046, at: 0, dur: 0.07 },
    { freq: 1568, at: 0.08, dur: 0.1 },
  ],
  complete: [
    { freq: 1046, at: 0, dur: 0.08 },
    { freq: 1318, at: 0.09, dur: 0.08 },
    { freq: 1568, at: 0.18, dur: 0.08 },
    { freq: 2093, at: 0.27, dur: 0.2 },
  ],
  notice: [{ freq: 880, at: 0, dur: 0.06, gain: 0.15 }],
  warning: [
    { freq: 660, at: 0, dur: 0.09, type: 'triangle' },
    { freq: 660, at: 0.14, dur: 0.09, type: 'triangle' },
  ],
  error: [
    { freq: 330, at: 0, dur: 0.14, type: 'square', gain: 0.12 },
    { freq: 220, at: 0.16, dur: 0.24, type: 'square', gain: 0.12 },
  ],
};

// ── Haptics ─────────────────────────────────────────────────────────────────

const VIBRATION: Record<ScanOutcome, number[]> = {
  success: [40],
  complete: [40, 60, 40, 60, 120],
  notice: [15],
  warning: [60, 80, 60],
  error: [180, 80, 180],
};

// iOS Safari 18+ fires a haptic tap when a `<input type="checkbox" switch>`
// toggles. Simulate a vibration pattern as one tap per pulse.
function iosTap() {
  try {
    const label = document.createElement('label');
    label.setAttribute('aria-hidden', 'true');
    label.style.display = 'none';
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.setAttribute('switch', '');
    label.appendChild(input);
    document.body.appendChild(label);
    label.click();
    label.remove();
  } catch { /* not supported */ }
}

function vibrate(pattern: number[]) {
  if (typeof navigator.vibrate === 'function') {
    try { navigator.vibrate(pattern); } catch { /* blocked */ }
    return;
  }
  // Odd indices are pauses; tap at the start of each pulse.
  let at = 0;
  pattern.forEach((ms, i) => {
    if (i % 2 === 0) setTimeout(iosTap, at);
    at += ms;
  });
}

// ── Public API ──────────────────────────────────────────────────────────────

/** The camera decoded a barcode; the verdict is still in flight. */
export function scanDecoded(): void {
  decodeSeq++;
  vibrate([12]);
}

/** Announce a scan's verdict: tone, vibration and full-screen flash. */
export function scanFeedback(outcome: ScanOutcome): void {
  pulseOutcome = outcome;
  pulseSeq++;
  play(TONES[outcome]);
  vibrate(VIBRATION[outcome]);
}
