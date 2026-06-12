// Custom Milkdown node: a partrack "part chip" — an inline, atomic reference to
// a Part. It round-trips losslessly through markdown as a remark *text directive*
// (`:part[label]{#id}`) and renders, while editing, as a mounted Svelte component
// (the node-view bridge we wanted to prove out).
// Milkdown's factory helpers are `$`-prefixed; rune-mode (.svelte.ts) forbids
// `$`-prefixed local bindings, so alias them on import.
import { $node as defNode, $view as defView, $remark as defRemark } from '@milkdown/kit/utils';
import type { Node as ProseNode } from '@milkdown/kit/prose/model';
import type { EditorView, NodeView } from '@milkdown/kit/prose/view';
import remarkDirective from 'remark-directive';
import { mount, unmount } from 'svelte';
import PartChip from './PartChip.svelte';

// Registers micromark/mdast directive support so `:part[...]{...}` is parsed
// into / serialized from a `textDirective` node. Shared by the read-only
// renderer later — directive parsing is defined once at the remark layer.
export const directiveRemark = defRemark('partrack-directive', () => remarkDirective);

interface DirectiveNode {
  type: string;
  name?: string;
  attributes?: Record<string, string | null | undefined>;
  children?: { type: string; value?: string }[];
}

export const partChipNode = defNode('part_chip', () => ({
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,
  attrs: {
    id: { default: '' },
    label: { default: '' },
  },
  parseDOM: [
    {
      tag: 'span[data-part-chip]',
      getAttrs: (dom) => {
        const el = dom as HTMLElement;
        return {
          id: el.getAttribute('data-id') ?? '',
          label: el.getAttribute('data-label') ?? el.textContent ?? '',
        };
      },
    },
  ],
  toDOM: (node) => [
    'span',
    {
      'data-part-chip': 'true',
      'data-id': node.attrs.id,
      'data-label': node.attrs.label,
    },
    node.attrs.label,
  ],
  parseMarkdown: {
    match: (node) => {
      const n = node as unknown as DirectiveNode;
      return n.type === 'textDirective' && n.name === 'part';
    },
    runner: (state, node, type) => {
      const n = node as unknown as DirectiveNode;
      const id = n.attributes?.id ?? '';
      const label = n.children?.[0]?.value ?? n.attributes?.label ?? '';
      state.addNode(type, { id, label });
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'part_chip',
    runner: (state, node) => {
      state.addNode(
        'textDirective',
        [{ type: 'text', value: node.attrs.label }] as never,
        undefined,
        { name: 'part', attributes: { id: node.attrs.id } },
      );
    },
  },
}));

// Node-view: mount the Svelte <PartChip> into the node's DOM and keep its props
// in sync when the node's attrs change. Svelte owns the inner DOM, so we tell
// ProseMirror to ignore mutations within it.
export const partChipView = defView(partChipNode, () => {
  return ((node: ProseNode, _view: EditorView): NodeView => {
    const dom = document.createElement('span');
    dom.setAttribute('data-part-chip', 'true');
    dom.style.whiteSpace = 'nowrap';

    const props = $state({ id: node.attrs.id as string, label: node.attrs.label as string });
    const component = mount(PartChip, { target: dom, props });

    return {
      dom,
      update: (updated) => {
        if (updated.type.name !== 'part_chip') return false;
        props.id = updated.attrs.id;
        props.label = updated.attrs.label;
        return true;
      },
      stopEvent: () => false,
      ignoreMutation: () => true,
      destroy: () => {
        unmount(component);
      },
    };
  }) as never;
});
