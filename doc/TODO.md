# partrack TODOs

This document outlines outstanding work items, some of which are rather comprehensive. Do not attempt to implement the whole document in a single session!

## Persistent notifications and updates

Currently there is no way for the user to be notified about operations beyond the usual toasts. We need to introduce a more persistent and stateful notification system, which will be used to inform users about part arrivals, new pending items in a cart, and the likes. For this, 

    - Dashboard list
    - A notifications dropdown, available from the existing user menu on all pages.
    - Email delivery based on importance (defer)
        - Notification debounce for emails. Not every notification must be delivered instantly, and ideally notifications can be summarized / combined. Also necessary to prevent spam filters / rate limits from becoming an issue for email.

## Part substitution handling.

For when we run out of stock for a particular part, and want to find a suitable replacement.

Substitutes are used to either fulfill builds out of existing stock or incoming orders with excess parts, but when not enough stock is avaialble the primary component should be added to the cart, as is already the case.
Substitutions should be defined at the BOM line item.
A build should not be fulfilled with two different parts from one line item, a substitute should only be used when it can completely fulfill the required demand. (Avoid having to deal with split line items).

In the bom detail view, we will need some "Manage Substitutes" function, which should be very similar in functionality to the Review Modal. Can the two maybe be merged? Should subsitute definition be part of the review process?
As for the UI, I imagine it as follows: We introduce a Manage Substitutes button for each line item.
The user should be presented with the following options in a new manage substitutes modal:
- Manage the list of current substitutes (Ordering, remove)
- Import subsitutes for this part from a previous revision (or previous build with it's revision). For this, only show revisions that actually included the part in question, and had subsitutes assigned to it.
- Add substitutes parts: Open the parts search modal we defined above, pre-populating search parameters with the primary parameters for the specified category. This will alow the user to select either a part from the local catalog (Default), or search a distributor. If a part from a distributor is selected, it will be imported into the local catalog, as is the case with other operations.

Builds will handle part substitutes as follows:

As builds start from a stock check, during the stock check we will verify whether we can supply the requested parts demand using the primary component, or if subsitute parts are available. The following situations should be considered:
- The primary part has enough inventory, no change from current behavior.
- The primary part does not have enough inventory:
    - There are no subsitutes: No change from current behavior
    - There are substitutes
        - but they are also not in stock: No change from current behavior
        - They are in stock, in sufficient quantity: The build will use the substitute part
The decision which is made during the stock check is saved into the build, but not fixed yet. As long as the build is in Draft stage, the user will have the opportunity to select which substitute (or primary) to use. When a different sub/primary is selected, the stock calculations and pricing calculations must be redone.
Once the user is satisfied with the current configuration, the normal ordering / checkout process begins, as implemented previously (Just taking the subsitute choice into account). At this point, the build becomes locked and can no longer be reconfigued - easing the ordering process. It can however still be cancelled.
Make sure that Parts arrivals are still be booked to the appropriate build line item.


# Leftover work items

- Check parts reservation
- ~~Figure out why build is not ready with plenty of local stock~~ — fixed: SubmitBuild now reserves free (non-reserved) stock and promotes fully-covered builds straight to `ready` instead of leaving them stuck in `pending`.
- Parameter management

- Convert partrack search (parameters!) to digikey search parameters

# Others

- Fix resolution of unresolved scans.
- Make activity log a *general* activity log, not just for scans