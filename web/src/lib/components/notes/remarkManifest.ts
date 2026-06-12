import type { NoteManifest } from '$lib/api/notes.js';

export interface ManifestOptions {
  manifest: NoteManifest;
  teamId: string;
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

// remarkManifest resolves a note's references against the server-provided
// manifest at transform time, baking the result into hast properties so the
// renderer components stay dumb (no render-time lookups / no extra requests):
//   - `:part[label]{#uuid}` text directives → a <part-chip> element carrying the
//     part's current mpn/lifecycle and a link href.
//   - `attachment:<uuid>` image sources → the resolved blob URL + dimensions.
// (Images inserted by the editor today already carry a real blob URL, so the
// attachment-scheme rewrite is forward-compat for the relocatable/S3 backend.)
export function remarkManifest(options: ManifestOptions) {
  const { manifest, teamId } = options;

  function visit(node: MdNode) {
    if ((node.type === 'textDirective' || node.type === 'leafDirective') && node.name === 'part') {
      applyPart(node);
    } else if (node.type === 'image' && typeof node.url === 'string' && node.url.startsWith('attachment:')) {
      applyImage(node);
    }
    if (Array.isArray(node.children)) {
      for (const child of node.children) visit(child);
    }
  }

  function applyPart(node: MdNode) {
    const id = (node.attributes?.id ?? '').toLowerCase();
    const label = node.children?.[0]?.value ?? node.attributes?.label ?? id;
    const part = id ? manifest.parts[id] : undefined;
    node.data = node.data ?? {};
    node.data.hName = 'part-chip';
    node.data.hProperties = {
      partId: id,
      label,
      mpn: part?.mpn ?? null,
      lifecycle: part?.lifecycle_status ?? null,
      href: id ? `/teams/${teamId}/parts/${id}` : null,
      resolved: part ? 'true' : 'false',
    };
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
