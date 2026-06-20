# Getting started

partrack is an electronics parts inventory system for PCB designers. It tracks
the parts you own, resolves the bills of materials (BOMs) from your PCB tools
into real catalog parts, tells you what you're short, helps you order it, and
keeps your stock accurate as you receive parts and assemble boards.

This chapter orients you in the application. If you'd rather learn by doing, jump
straight to [Your first project](first-project.md) for a step-by-step walk
through the whole workflow.

## What partrack is for

At its heart partrack closes the loop between a design and the physical parts:

1. **Import a BOM** from your PCB tool into a [project](projects-bom.md).
2. partrack **resolves** each BOM line to a catalog [part](parts-inventory.md).
3. A **stock check** shows what's in stock and where you fall short for a given
   build quantity.
4. Shortfalls go into the team [shopping cart](cart.md), which becomes
   [orders](orders.md) grouped by distributor.
5. You **receive** orders; stock updates and parts are enriched with distributor
   data.
6. A [build](builds.md) deducts the parts consumed when you assemble boards.

You don't have to use the whole loop. Many teams also use partrack just to track
inventory, [scan](scanning.md) parts in, and check them out ad hoc.

## Signing in

partrack supports a few sign-in methods, depending on how your administrator set
the instance up:

- **Email and password** — local accounts.
- **Single sign-on (OIDC)** — log in through your organisation's identity
  provider.
- **Directory login (LDAP)** — sign in with your directory credentials.

However you sign in, you land on the same account, with the same teams and
history. You can manage your devices and tokens from your account settings
(below). See [Administration](administration.md) for how these methods are
configured.

## Teams

Almost everything in partrack belongs to a **team**: parts, inventory, projects,
the cart, orders, and builds are all scoped to the team you're working in. If you
belong to more than one team, use the **team switcher** at the top of the
sidebar to change context.

Your permissions can differ per team — you might be able to place orders in one
team and only browse in another. See [Roles & permissions](roles-and-permissions.md)
for what each role can do.

## Finding your way around

- **Sidebar** — the primary navigation, with sections for Parts, Storage,
  Projects, Cart, Orders, Builds and Checkouts, plus team settings and (for
  instance admins) the Admin area.
- **Global search** — press `⌘K` (or `Ctrl-K`), or click the search bar in the
  top bar, to jump to any part, project, order or page from anywhere.
- **Dashboard** — the team landing page summarises stock alerts, work in
  progress and recent activity.
- **Live updates** — partrack pushes changes to your screen as they happen, so
  when a teammate receives an order or edits a part you see it without
  refreshing. A connection indicator shows when the live link is active.

## Getting help

The **?** icon in the top bar opens this guide to the chapter for the page
you're on. From the help panel you can also browse every other chapter using the
list on the left. Whenever you're unsure what a page does, that's the fastest
way to find out.

## Your account

Open your account from the bottom of the sidebar:

- **Profile** — your display name and password.
- **Sessions** — the devices currently signed in; revoke any you don't
  recognise.
- **API tokens** — create tokens for scripts or integrations that act on the
  partrack API as you. A token carries your permissions, so treat it like a
  password.

The running partrack version is shown at the very bottom of the sidebar; hover
it to see the exact build. After an update you can read what changed in the
release notes.

## Where to go next

- New to partrack? Follow [Your first project](first-project.md).
- Looking after the catalog and stock? See [Parts & inventory](parts-inventory.md)
  and [Storage](storage.md).
- Working from designs? See [Projects & BOMs](projects-bom.md).
- Buying and building? See [Shopping cart](cart.md), [Orders](orders.md) and
  [Builds](builds.md).
- Running the instance? See [Administration](administration.md).
