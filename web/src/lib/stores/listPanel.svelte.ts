// Shell-level state for the floating list panel. Lives above the router
// outlet so the panel survives navigation; persisted per team so the open
// panel and active list come back after a reload.

export interface ListPanelState {
  teamId: string;
  open: boolean;
  activeListId: string | null;
}

export const listPanelStore = $state<ListPanelState>({
  teamId: '',
  open: false,
  activeListId: null,
});

const storageKey = (teamId: string) => `partrack:list-panel:${teamId}`;

function persist(): void {
  if (!listPanelStore.teamId) return;
  try {
    localStorage.setItem(
      storageKey(listPanelStore.teamId),
      JSON.stringify({ open: listPanelStore.open, activeListId: listPanelStore.activeListId }),
    );
  } catch {
    // storage unavailable (private mode etc.) — panel state is just not sticky
  }
}

/** Switches the store to a team, restoring that team's persisted panel state. */
export function loadListPanel(teamId: string): void {
  listPanelStore.teamId = teamId;
  listPanelStore.open = false;
  listPanelStore.activeListId = null;
  if (!teamId) return;
  try {
    const raw = localStorage.getItem(storageKey(teamId));
    if (raw) {
      const saved = JSON.parse(raw) as { open?: boolean; activeListId?: string | null };
      listPanelStore.open = saved.open ?? false;
      listPanelStore.activeListId = saved.activeListId ?? null;
    }
  } catch {
    // corrupt entry — fall back to defaults
  }
}

export function setListPanelOpen(open: boolean): void {
  listPanelStore.open = open;
  persist();
}

export function setActiveList(id: string | null): void {
  listPanelStore.activeListId = id;
  persist();
}
