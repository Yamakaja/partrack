// Slash-command plugin: drives a Svelte <SlashMenu> via Milkdown's SlashProvider.
// The two partrack-native hooks live here — "Link part" inserts a part_chip node,
// "Insert image" uploads to /attachments and inserts an image node. Both first
// delete the typed "/query" trigger text.
import { slashFactory, SlashProvider } from '@milkdown/kit/plugin/slash';
import type { EditorView } from '@milkdown/kit/prose/view';
import type { EditorState } from '@milkdown/kit/prose/state';
import { mount, unmount } from 'svelte';
import SlashMenu from './SlashMenu.svelte';
import type { SlashActions } from './types.js';
import { uploadAttachment } from '$lib/api/attachments.js';
import type { ObjectKind } from '$lib/components/notes/objectKinds.js';

export const slash = slashFactory('partrack-slash');

interface Trigger {
  query: string;
  len: number; // chars to delete on insert: "/" + query
  key: string; // cursor pos + query — identifies a specific open menu
}

export function slashView(teamId: string) {
  return (view: EditorView) => {
    // SlashProvider only flips a `data-show` attribute on this element and calls
    // onShow/onHide — it does NOT hide it. We own the actual show/hide (display)
    // and floating positioning anchors off it.
    const content = document.createElement('div');
    content.style.position = 'absolute';
    content.style.zIndex = '50';
    content.style.display = 'none';
    let currentView = view;
    let triggerLen = 1;
    // Once dismissed (Escape / click-outside), suppress this exact trigger until
    // the user edits or moves — otherwise the next shouldShow reopens it.
    let dismissedKey: string | null = null;

    const props = $state<{ teamId: string; query: string; actions: SlashActions }>({
      teamId,
      query: '',
      actions: { insertObject, insertImageFile, close },
    });

    // Reads the "/query" token immediately before an empty cursor, if any.
    function matchTrigger(v: EditorView): Trigger | null {
      const { selection } = v.state;
      if (!selection.empty) return null;
      const resolved = selection.$from;
      const textBefore = resolved.parent.textBetween(0, resolved.parentOffset, undefined, '￼');
      const m = /(?:^|\s)\/([\w-]*)$/.exec(textBefore);
      if (!m) return null;
      const query = m[1] ?? '';
      return { query, len: query.length + 1, key: `${resolved.pos}|${query}` };
    }

    function deleteTrigger(v: EditorView) {
      const to = v.state.selection.from;
      const from = Math.max(0, to - triggerLen);
      return v.state.tr.delete(from, to);
    }

    function close() {
      const t = matchTrigger(currentView);
      dismissedKey = t ? t.key : null;
      provider.hide();
    }

    function insertObject(kind: ObjectKind, id: string, label: string) {
      const v = currentView;
      const type = v.state.schema.nodes.obj_link;
      if (!type) return;
      const node = type.create({ kind, id, label });
      v.dispatch(deleteTrigger(v).replaceSelectionWith(node, false));
      provider.hide();
      v.focus();
    }

    async function insertImageFile(file: File) {
      const v = currentView;
      v.dispatch(deleteTrigger(v));
      provider.hide();
      v.focus();
      try {
        const a = await uploadAttachment(teamId, file);
        const type = v.state.schema.nodes.image;
        const node = type.createAndFill({ src: a.url, alt: a.filename ?? file.name });
        if (node) currentView.dispatch(currentView.state.tr.replaceSelectionWith(node, false));
      } catch (e) {
        console.error('image upload failed', e);
      }
    }

    const component = mount(SlashMenu, { target: content, props });

    const provider = new SlashProvider({
      content,
      trigger: '/',
      shouldShow(v: EditorView) {
        const t = matchTrigger(v);
        if (!t) return false;
        if (t.key === dismissedKey) return false;
        props.query = t.query;
        triggerLen = t.len;
        return true;
      },
    });

    // Dismissal: the menu (esp. the part-search sub-mode) steals focus from the
    // editor, so no editor update fires to hide it. Listen globally while shown.
    function onPointerDown(e: PointerEvent) {
      if (!content.contains(e.target as Node)) close();
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        close();
        currentView.focus();
      }
    }

    provider.onShow = () => {
      content.style.display = '';
      (component as { reset?: () => void }).reset?.();
      document.addEventListener('pointerdown', onPointerDown, true);
      document.addEventListener('keydown', onKeyDown, true);
    };
    provider.onHide = () => {
      content.style.display = 'none';
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('keydown', onKeyDown, true);
    };

    return {
      update: (updatedView: EditorView, prevState?: EditorState) => {
        currentView = updatedView;
        provider.update(updatedView, prevState);
      },
      destroy: () => {
        document.removeEventListener('pointerdown', onPointerDown, true);
        document.removeEventListener('keydown', onKeyDown, true);
        provider.destroy();
        unmount(component);
      },
    };
  };
}
