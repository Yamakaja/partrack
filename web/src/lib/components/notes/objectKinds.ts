// Note-editor entry point to the object-kind registry.
//
// The registry itself lives in $lib/objectref/registry (kinds, icons, statuses,
// labels, routes), mirroring internal/objectref. This module only re-exports it
// under the path the editor and manifest code already import, so there is one
// definition of what a kind is and how it is drawn.

export type { ObjectKind } from '$lib/types/objectRef.js';
export { OBJECT_KINDS } from '$lib/types/objectRef.js';
export { KIND_META, kindIcon, type KindMeta } from '$lib/objectref/registry.js';
