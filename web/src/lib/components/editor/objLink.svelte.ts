// Custom Milkdown node: a partrack "object link" — an inline, atomic reference
// to any linkable object (part/project/revision/order/build/storage). It
// round-trips through markdown as a remark *text directive* whose name is the
// object kind (`:project[label]{#uuid}`) and renders, while editing, as a
// mounted Svelte chip (the node-view bridge).
import { $node as defNode, $view as defView, $remark as defRemark } from '@milkdown/kit/utils';
import type { Node as ProseNode } from '@milkdown/kit/prose/model';
import type { EditorView, NodeView } from '@milkdown/kit/prose/view';
import remarkDirective from 'remark-directive';
import { mount, unmount } from 'svelte';
import ObjectChip from './ObjectChip.svelte';
import { OBJECT_KINDS, type ObjectKind } from '$lib/components/notes/objectKinds.js';

// Registers micromark/mdast directive support, shared with the viewer's remark
// pipeline (directive parsing defined once at the remark layer).
export const directiveRemark = defRemark('partrack-directive', () => remarkDirective);

const KINDS = new Set<string>(OBJECT_KINDS);

interface DirectiveNode {
  type: string;
  name?: string;
  attributes?: Record<string, string | null | undefined>;
  children?: { type: string; value?: string }[];
}

export const objLinkNode = defNode('obj_link', () => ({
  group: 'inline',
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,
  attrs: {
    kind: { default: 'part' },
    id: { default: '' },
    label: { default: '' },
  },
  parseDOM: [
    {
      tag: 'span[data-obj-link]',
      getAttrs: (dom) => {
        const el = dom as HTMLElement;
        return {
          kind: el.getAttribute('data-kind') ?? 'part',
          id: el.getAttribute('data-id') ?? '',
          label: el.getAttribute('data-label') ?? el.textContent ?? '',
        };
      },
    },
  ],
  toDOM: (node) => [
    'span',
    {
      'data-obj-link': 'true',
      'data-kind': node.attrs.kind,
      'data-id': node.attrs.id,
      'data-label': node.attrs.label,
    },
    node.attrs.label,
  ],
  parseMarkdown: {
    match: (node) => {
      const n = node as unknown as DirectiveNode;
      return n.type === 'textDirective' && !!n.name && KINDS.has(n.name);
    },
    runner: (state, node, type) => {
      const n = node as unknown as DirectiveNode;
      const kind = n.name ?? 'part';
      const id = n.attributes?.id ?? '';
      const label = n.children?.[0]?.value ?? n.attributes?.label ?? '';
      state.addNode(type, { kind, id, label });
    },
  },
  toMarkdown: {
    match: (node) => node.type.name === 'obj_link',
    runner: (state, node) => {
      state.addNode(
        'textDirective',
        [{ type: 'text', value: node.attrs.label }] as never,
        undefined,
        { name: node.attrs.kind, attributes: { id: node.attrs.id } },
      );
    },
  },
}));

export const objLinkView = defView(objLinkNode, () => {
  return ((node: ProseNode, _view: EditorView): NodeView => {
    const dom = document.createElement('span');
    dom.setAttribute('data-obj-link', 'true');
    dom.style.whiteSpace = 'nowrap';

    const props = $state({ kind: node.attrs.kind as ObjectKind, label: node.attrs.label as string });
    const component = mount(ObjectChip, { target: dom, props });

    return {
      dom,
      update: (updated) => {
        if (updated.type.name !== 'obj_link') return false;
        props.kind = updated.attrs.kind;
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
