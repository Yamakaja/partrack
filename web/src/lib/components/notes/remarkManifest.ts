import type { NoteManifest } from '$lib/api/notes.js';
import { OBJECT_KINDS } from './objectKinds.js';

export interface ManifestOptions {
  manifest: NoteManifest;
}

// Loose mdast node — we only touch a few known shapes.
interface MdNode {
  type: string;
  name?: string;
  url?: string;
  attributes?: Record<string, string | null | undefined>;
  children?: MdNode[];
  value?: string;
  data?: { hName?: string; hProperties?: Record<string, unknown> };
}

const KINDS = new Set<string>(OBJECT_KINDS);

// remarkManifest resolves a note's references against the server-provided
// manifest at transform time, baking the result into hast properties so the
// renderer components stay dumb (no render-time lookups / no extra requests):
//   - `:<kind>[label]{#uuid}` text directives → an <obj-link> element carrying
//     the object's resolved kind, label, href, and status. Unresolved refs keep
//     the directive's own label and render struck-through with no link.
//   - `attachment:<uuid>` image sources → the resolved blob URL + dimensions.
export function remarkManifest(options: ManifestOptions) {
  const { manifest } = options;

  function visit(node: MdNode) {
    if ((node.type === 'textDirective' || node.type === 'leafDirective') && node.name && KINDS.has(node.name)) {
      applyObject(node);
    } else if (node.type === 'image' && typeof node.url === 'string' && node.url.startsWith('attachment:')) {
      applyImage(node);
    }
    if (Array.isArray(node.children)) {
      for (const child of node.children) visit(child);
    }
  }

  function applyObject(node: MdNode) {
    const id = (node.attributes?.id ?? '').toLowerCase();
    const fallbackLabel = node.children?.[0]?.value ?? node.attributes?.label ?? id;
    const obj = id ? manifest.objects[id] : undefined;
    node.data = node.data ?? {};
    node.data.hName = 'obj-link';
    node.data.hProperties = obj
      ? { kind: obj.kind, label: obj.label, href: obj.href, status: obj.status ?? null, exists: 'true' }
      : { kind: node.name ?? 'part', label: fallbackLabel, href: null, status: null, exists: 'false' };
    // Atom node: drop the directive's label children so the renderer doesn't
    // also emit them as loose text next to the chip.
    node.children = [];
  }

  function applyImage(node: MdNode) {
    const id = (node.url ?? '').slice('attachment:'.length).toLowerCase();
    const att = manifest.attachments[id];
    if (!att) return;
    node.url = att.url;
    node.data = node.data ?? {};
    node.data.hProperties = {
      ...(node.data.hProperties ?? {}),
      ...(att.width ? { width: att.width } : {}),
      ...(att.height ? { height: att.height } : {}),
    };
  }

  return (tree: MdNode) => visit(tree);
}
