export interface SseEvent {
  type: string;
  job_id: string;
  team_id: string;
  data: Record<string, unknown>;
}

type SseCallback = (event: SseEvent) => void;

let currentTeamId: string | null = null;
let es: EventSource | null = null;
const subscribers = new Map<string, SseCallback[]>(); // keyed by job_id

function openForTeam(teamId: string) {
  if (currentTeamId === teamId && es && es.readyState !== EventSource.CLOSED) return;
  closeSSE();
  currentTeamId = teamId;
  es = new EventSource(`/api/v1/teams/${teamId}/events`, { withCredentials: true });
  es.onmessage = (e) => {
    try {
      const event: SseEvent = JSON.parse(e.data);
      const cbs = subscribers.get(event.job_id) ?? [];
      cbs.forEach((cb) => cb(event));
    } catch {
      /* ignore malformed */
    }
  };
  es.onerror = () => {
    /* EventSource auto-reconnects */
  };
}

export function subscribeSSE(
  teamId: string,
  jobId: string,
  callback: SseCallback,
): () => void {
  openForTeam(teamId);
  const existing = subscribers.get(jobId) ?? [];
  subscribers.set(jobId, [...existing, callback]);
  return () => {
    const cbs = subscribers.get(jobId) ?? [];
    const filtered = cbs.filter((cb) => cb !== callback);
    if (filtered.length === 0) subscribers.delete(jobId);
    else subscribers.set(jobId, filtered);
  };
}

export function closeSSE() {
  es?.close();
  es = null;
  currentTeamId = null;
  subscribers.clear();
}
