#!/usr/bin/env python3
"""
Validates that the category eval files have complete coverage:
  1. top-level-eval.json covers all top-level categories in categories.json
  2. subcategory-eval.json covers all kept top-level categories and all their children
  3. grandchildren-eval.json covers all kept subcategories and all their children
"""

import json
import sys
from pathlib import Path

BASE = Path(__file__).parent.parent / "doc/work/categories"

def load(name):
    with open(BASE / name) as f:
        return json.load(f)

def check(label, expected_ids: set, actual_ids: set, id_to_name: dict = None):
    missing = expected_ids - actual_ids
    extra   = actual_ids - expected_ids
    ok = True
    if missing:
        print(f"[FAIL] {label}: {len(missing)} missing:")
        for i in sorted(missing):
            name = f"  ({id_to_name[i]})" if id_to_name and i in id_to_name else ""
            print(f"       - {i}{name}")
        ok = False
    if extra:
        print(f"[WARN] {label}: {len(extra)} extra (not in source):")
        for i in sorted(extra):
            name = f"  ({id_to_name[i]})" if id_to_name and i in id_to_name else ""
            print(f"       + {i}{name}")
        ok = False
    if ok:
        print(f"[ OK ] {label}: {len(expected_ids)} entries covered")
    return ok

def main():
    categories    = load("categories.json")["Categories"]
    top_eval      = load("top-level-eval.json")
    sub_eval      = load("subcategory-eval.json")
    grand_eval    = load("grandchildren-eval.json")

    # --- Level 1: top-level categories ---
    source_top_ids = {c["CategoryId"] for c in categories}
    eval_top_ids   = {e["id"] for e in top_eval}
    # Build a flat id→name map for all categories (populated after index_all below)
    all_cats_by_id = {}
    def index_all(nodes):
        for n in nodes:
            all_cats_by_id[n["CategoryId"]] = n
            index_all(n.get("Children", []))
    index_all(categories)
    id_name = {cid: c["Name"] for cid, c in all_cats_by_id.items()}

    ok1 = check("top-level-eval vs categories.json top-level", source_top_ids, eval_top_ids, id_name)

    # --- Level 2: subcategory-eval must cover all kept top-level categories ---
    kept_top_ids = {e["id"] for e in top_eval if e["keep"]}

    # Build map: top_id -> set of child CategoryIds from categories.json
    cat_by_id = {c["CategoryId"]: c for c in categories}
    source_children_by_top = {
        top_id: {ch["CategoryId"] for ch in cat_by_id[top_id]["Children"]}
        for top_id in kept_top_ids
        if top_id in cat_by_id
    }

    # Build map from sub_eval
    eval_children_by_top = {}
    for entry in sub_eval:
        eval_children_by_top[entry["top_id"]] = {ch["id"] for ch in entry["children"]}

    # Check that every kept top-level has an entry in sub_eval
    eval_sub_top_ids = set(eval_children_by_top.keys())
    ok2a = check("subcategory-eval: covered top-level IDs", kept_top_ids, eval_sub_top_ids, id_name)

    # Check that children sets match for each top-level present in both
    ok2b = True
    for top_id in kept_top_ids & eval_sub_top_ids:
        expected = source_children_by_top.get(top_id, set())
        actual   = eval_children_by_top[top_id]
        name     = cat_by_id[top_id]["Name"] if top_id in cat_by_id else str(top_id)
        if not check(f"subcategory-eval children of '{name}' (id={top_id})", expected, actual, id_name):
            ok2b = False

    # --- Level 3: grandchildren-eval must cover all kept subcategories that have grandchildren ---
    # Collect all kept child ids from sub_eval that actually have children in categories.json
    kept_child_ids = set()
    for entry in sub_eval:
        for ch in entry["children"]:
            if ch["keep"]:
                cid = ch["id"]
                if cid in all_cats_by_id and all_cats_by_id[cid]["Children"]:
                    kept_child_ids.add(cid)

    # Build map: child_id -> set of grandchild CategoryIds from categories.json
    source_grandchildren_by_child = {
        child_id: {gc["CategoryId"] for gc in all_cats_by_id[child_id]["Children"]}
        for child_id in kept_child_ids
    }

    # Build map from grand_eval
    eval_grandchildren_by_child = {
        entry["child_id"]: {gc["id"] for gc in entry["grandchildren"]}
        for entry in grand_eval
    }

    eval_grand_child_ids = set(eval_grandchildren_by_child.keys())
    ok3a = check("grandchildren-eval: covered subcategory IDs", kept_child_ids, eval_grand_child_ids, id_name)

    ok3b = True
    for child_id in kept_child_ids & eval_grand_child_ids:
        expected = source_grandchildren_by_child.get(child_id, set())
        actual   = eval_grandchildren_by_child[child_id]
        name     = all_cats_by_id[child_id]["Name"] if child_id in all_cats_by_id else str(child_id)
        if not check(f"grandchildren-eval children of '{name}' (id={child_id})", expected, actual, id_name):
            ok3b = False

    all_ok = ok1 and ok2a and ok2b and ok3a and ok3b
    print()
    print("PASS" if all_ok else "FAIL")
    sys.exit(0 if all_ok else 1)

if __name__ == "__main__":
    main()
