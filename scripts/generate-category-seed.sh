#!/usr/bin/env bash
# generate-category-seed.sh
#
# Regenerates db/migrations/000015_category_seed.up.sql from the evaluated
# category hierarchy. Edit the eval files in doc/work/categories/ to adjust
# which DigiKey categories are included, then re-run this script.
#
# Inputs (edit these to adjust the output):
#   doc/work/categories/top-level-eval.json       — which top-level DigiKey categories to keep
#   doc/work/categories/batch{1..4}-eval.json      — which subcategories to keep (per top-level batch)
#   doc/work/categories/grandchildren-batch{1,2,3}-eval.json — which grandchildren to keep
#
# Outputs (auto-generated, do not edit by hand):
#   doc/work/categories/subcategory-eval.json      — merged subcategory evals
#   doc/work/categories/grandchildren-eval.json    — merged grandchild evals
#   doc/work/categories/hierarchy.json             — filtered DigiKey tree
#   doc/work/categories/partrack-categories.json       — partrack IDs + slugs (pre-fix)
#   doc/work/categories/partrack-categories-fixed.json — partrack IDs + slug fixes + mappings
#   db/migrations/000015_category_seed.up.sql      — final SQL

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WORK="$REPO_ROOT/doc/work/categories"
CATS="$WORK/categories.json"
MIGRATION="$REPO_ROOT/db/migrations/000015_category_seed.up.sql"

echo "==> Building filtered hierarchy from DigiKey data..."
jq --slurpfile top "$WORK/top-level-eval.json" \
   --slurpfile sub "$WORK/subcategory-eval.json" \
   --slurpfile gc  "$WORK/grandchildren-eval.json" \
'
($gc[0] | map({(.child_id | tostring): .grandchildren}) | add) as $gc_map |

[$top[0][] | select(.keep) | {
  id,
  name,
  children: (
    .id as $tid |
    [$sub[0][] | select(.top_id == $tid) | .children[] | select(.keep) | {
      id,
      name,
      grandchildren: (
        .id as $cid |
        if ($gc_map[($cid | tostring)]) then
          [$gc_map[($cid | tostring)][] | select(.keep) | {id, name}]
        else [] end
      )
    }]
  )
}]
' "$CATS" > "$WORK/hierarchy.json"

echo "==> Assigning partrack IDs, generating slugs and mappings..."
jq '
(
  [.[] | .id] | to_entries | map({key: (.value|tostring), value: (.key+1)}) | from_entries
) as $top_ids |
(
  [.[] | .children[] | .id] | to_entries | map({key: (.value|tostring), value: (.key+101)}) | from_entries
) as $child_ids |
(
  [.[] | .children[] | .grandchildren[] | .id] | to_entries | map({key: (.value|tostring), value: (.key+1001)}) | from_entries
) as $gc_ids |

def slugify: ascii_downcase | gsub("[(),/+]"; " ") | gsub("[^a-z0-9 -]"; "") | gsub(" +"; "-") | gsub("-+"; "-") | gsub("^-|-$"; "");

{
  categories: (
    [.[] | {
      id: $top_ids[(.id|tostring)],
      digikey_id: .id,
      name,
      slug: (.name | slugify),
      parent_id: null,
      sort_order: $top_ids[(.id|tostring)]
    }] +
    [.[] | .id as $top_dk | .children | to_entries[] | {
      id: $child_ids[(.value.id|tostring)],
      digikey_id: .value.id,
      name: .value.name,
      slug: (.value.name | slugify),
      parent_id: $top_ids[($top_dk|tostring)],
      sort_order: (.key+1)
    }] +
    [.[] | .children[] | .id as $child_dk | .grandchildren | to_entries[] | {
      id: $gc_ids[(.value.id|tostring)],
      digikey_id: .value.id,
      name: .value.name,
      slug: (.value.name | slugify),
      parent_id: $child_ids[($child_dk|tostring)],
      sort_order: (.key+1)
    }]
    | sort_by(.id)
  ),
  mappings: (
    [.[] | {digikey_id: .id, partrack_id: $top_ids[(.id|tostring)]}] +
    [.[] | .children[] | {digikey_id: .id, partrack_id: $child_ids[(.id|tostring)]}] +
    [.[] | .children[] | .grandchildren[] | {digikey_id: .id, partrack_id: $gc_ids[(.id|tostring)]}]
  )
}
' "$WORK/hierarchy.json" > "$WORK/partrack-categories.json"

echo "==> Slug collision report (pre-fix)..."
jq -r '
(.categories | map({(.id|tostring): .name}) | add) as $names |
(.categories | group_by(.slug) | map(select(length > 1))) as $dupes |
if ($dupes | length) == 0 then "  No collisions."
else $dupes[] | "  \(.[0].slug)", (.[] | "    \(.id)  \(.name)  (\(if .parent_id then $names[(.parent_id|tostring)] else "top level" end))")
end
' "$WORK/partrack-categories.json"

echo "==> Applying slug fixes..."
jq '
# Slug disambiguation: IDs whose auto-generated slug collides with another category.
# Key: partrack ID (as string), Value: corrected slug.
# Update this map whenever the hierarchy changes and new collisions appear.
{
  "295":  "board-mount-ac-dc-converters",
  "297":  "off-board-ac-dc-converters",
  "1067": "pmic-battery-chargers",
  "111":  "assembled-coaxial-cables-rf",
  "123":  "bare-coaxial-cables-rf",
  "294":  "board-mount-dc-dc-converters",
  "299":  "off-board-dc-dc-converters",
  "209":  "hardware-din-rail-channel",
  "1012": "terminal-blocks-din-rail",
  "119":  "assembled-fiber-optic-cables",
  "124":  "bare-fiber-optic-cables",
  "254":  "optics-filters",
  "296":  "board-mount-led-drivers",
  "301":  "off-board-led-drivers",
  "1063": "pmic-led-drivers",
  "252":  "optics-lenses",
  "283":  "optoelectronics-lenses",
  "1014": "terminal-blocks-specialized",
  "1028": "backplane-connectors-specialized",
  "1103": "interface-specialized",
  "1154": "test-leads-thermocouples",
  "1203": "memory-controllers",
  "1183": "interface-controllers",
  "103": "battery-charger-products",
  "1153": "pmics",
  "220": "memory-ics",
  "1204": "memory",
  "1018": "specialized-terminal-blocks",
  "1052": "specialized backplane connectors",
  "1189": "specialized-interfaces",
  "1220": "temperature-sensors-and-probes",
  "1264": "temperature-test-leads",
  "1003": "transient-voltage-suppressors"
} as $fixes |
.categories |= map(
  if $fixes[(.id|tostring)] then .slug = $fixes[(.id|tostring)] else . end
)
' "$WORK/partrack-categories.json" > "$WORK/partrack-categories-fixed.json"

# Verify no duplicate slugs remain
DUPES=$(jq '.categories | group_by(.slug) | map(select(length>1)) | length' "$WORK/partrack-categories-fixed.json")
if [ "$DUPES" -gt 0 ]; then
  echo "ERROR: $DUPES duplicate slug(s) remain after fixes:" >&2
  jq '.categories | group_by(.slug) | map(select(length>1)) | .[] | {slug: .[0].slug, ids: map(.id)}' "$WORK/partrack-categories-fixed.json" >&2
  exit 1
fi

echo "==> Generating SQL migration..."
jq -r '
"-- Partrack category seed",
"-- Auto-generated by scripts/generate-category-seed.sh — do not edit by hand",
"-- " + (.categories | length | tostring) + " categories, " + (.mappings | length | tostring) + " DigiKey mappings",
"",
"INSERT INTO categories (id, slug, display_name, parent_id, sort_order, primary_params) VALUES",
(
  [.categories[] |
    "  (" +
    (.id|tostring) + ", " +
    ("'\''" + .slug + "'\''") + ", " +
    ("'\''" + (.name | gsub("'\''"; "'\'''\''")) + "'\''") + ", " +
    (if .parent_id then (.parent_id|tostring) else "NULL" end) + ", " +
    (.sort_order|tostring) + ", " +
    "'\''[]'\''" +
    ")"
  ] | join(",\n")
),
";",
"",
"SELECT setval('\''categories_id_seq'\'', (SELECT MAX(id) FROM categories));",
"",
"-- DigiKey distributor category mappings",
"INSERT INTO distributor_category_mappings (distributor_id, distributor_category_id, category_id) VALUES",
(
  [.mappings[] |
    "  ((SELECT id FROM distributors WHERE name = '\''DigiKey'\''), " +
    (.digikey_id|tostring) + ", " +
    (.partrack_id|tostring) +
    ")"
  ] | join(",\n")
),
";"
' "$WORK/partrack-categories-fixed.json" > "$MIGRATION"

CATS_COUNT=$(jq '.categories | length' "$WORK/partrack-categories-fixed.json")
MAPS_COUNT=$(jq '.mappings | length' "$WORK/partrack-categories-fixed.json")
LINES=$(wc -l < "$MIGRATION")
echo "==> Done: $CATS_COUNT categories, $MAPS_COUNT mappings, $LINES lines → $MIGRATION"
