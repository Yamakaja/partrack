# partrack TODOs

This document outlines outstanding work items, some of which are rather comprehensive. Do not attempt to implement the whole document in a single session!

## Persistent notifications and updates

Currently there is no way for the user to be notified about operations beyond the usual toasts. We need to introduce a more persistent and stateful notification system, which will be used to inform users about part arrivals, new pending items in a cart, and the likes. For this, 

    - Dashboard list
    - A notifications dropdown, available from the existing user menu on all pages.
    - Email delivery based on importance (defer)
        - Notification debounce for emails. Not every notification must be delivered instantly, and ideally notifications can be summarized / combined. Also necessary to prevent spam filters / rate limits from becoming an issue for email.

## Generic advanced table component

Quite often we run into the problem that we have a bunch of data that we want to present to the user in the frontend. I would like to build a generic table component that we can use to build (almost) all of our tables in general. Note that some tables should not be replaced, either because they are only structurally a table, or because they have completely different formatting requirerments than the table which we are building here. For example, the PriceBreaksTable component should not be reworked with such a generic table component. However, things like parts lists, distributor lists, bom line items, etc. may all benefit from the generality and reuse of this table component.

First, we need to consider two different cases:
- The data may only be loaded in partially from the backend, with active paging functionality. This will for example be the appropriate choice for potentially large and continuously growing datasets like the parts catalog or stock adjustments.
- The data source may be fairly constrained, like revisions, builds, orders, and as such the frontend can be sent the whole dataset.

There are many common requirements for both scenarios, which who's implementation may differ drastically depending on the backend, which is why it might make sense to keep them separate. However maybe we can internally separate the component into a data source and presentation component, where the presentation component will be identical, but the data source can be exchanged for either a local or remote one (Or other custom sources confirming to the data source interface).

The general UX requirements for this table are:

- Standard table stuff like already implemented
- The ability to sort data by an arbitrary column
- The ability to filter data by available values in specific columns.
- Pagination (for remote datasets, probably not required for local datasets, as they should generally be constrained to a few hundreds of rows).
- The ability to export to csv. For local data this could be handled completely in the frontend, while for remote data this may have to be handled in the backend. We need to consider how to handle potentially large outputs

The following features will also be necessary:
- We must be able to define click actions on the row
- define detail panels (see CartItemDetail)
- We need an "actions" column at the end of the table to place buttons. Probably no header required for this, look at existing table implementations.
- There may have to be an option for style overrides, but lets defer this for now. One of the main ideas of this was to unify the styling and interactive functionality.

### Data Sources

The data sources will have to define some information about the types which are present in each column. We can probably narrow it down to numerical types, currency values, fixed list of options (Like current states of something) and freetext. There may also be unit information be attached with the numerical values, which will be useful for the parameter based searches later on. Similarly, data sources should have the ability to control features. In some tables maybe we really don't want to implementing filtering or sorting, or only by specific columns.

To enable code reuse, we should start by defining a basic local and remote data source, which can be extended for each specific use to provide the table with the required information, callbacks and actions.

As for the DataSource interface, maybe we can also differentiate between the paged and non-paged cases, which would leave three cases in total:

- GetPaged (With page number and page size)
- Get
- GetExport (See next subsection)

Filtering and sorting of course need to be handled aswell, potentially through a different function like GetFiltered, GetPagedFiltered etc, which are provided with a default filtering and sorting implementation and state though the provided dataset base classes.

We also need to think about how sorting and filtering state is kept in this situation. I would prefer if that was handled by the presentation layer, in concjunction with the ColumnSpecs it receives from the datasource.

Since we are dealing with a lot of data, all opportunities for caching should be used. The backend API calls should be cached already thanks to tanstack, but also things like the column specs (Available filter values / ranges) should not have to be recomputed too often.

The get data calls should be async, allowing the table to indicate that it's waiting for new data. (Spinner)

### CSV Export

The data export should also be an option for pretty much any table that we have, whether it may make sense or not. However that means tha this probably shouldn't be a big button, but a stuble icon that the may ignore.

Export can be disabled by the data source if necessary.

The export request is generally forwarded to the data source though a separate call, indicating to the data source that an export is in progress. The default implementations can translate the export call to just calling the normal (paged) sourcing functions within the data source, which are used to populate the visible columns, but custom data source implementations may also use this facility to provide additional data on export.

### General search

Sometimes a general filter option is also required (Imagine a search bar over the table). Here, the user would enter some identifying information like a part number or just part of a description, providing an additional filter on the table. Is this something that we should leave to the implementer of the custom data source object for that specific instance to handle? The cases here may vary a lot ...

## Builds dashboard

We need to introduce a card to the team dashboard which provides an overview of the current builds pipeline. Cancelled builds are not relevant, but the pipeline could for example be shown with a (full page wide) card showing a srollable list of builds for each stage of the pipeline. Include all the relevant information for a build, but make it compact. Imagine a structure like this: [ [Draft Builds] -> [Pending Builds] -> [etc.]], with each of the brackets indicating cards / the respective lists in the cards. For the rows, take inspiration from the way that search results are listed in the BomReviewDialog.svelte:348

## Additional parts information

We are currently not storing all the parts information that we receive from digikey (Note that this information may not be available from other distributors, so many of these things must remain optional, but are useful when available)

### Basic improvements to the stored and shown data

- Update part information to also use / track:
    - Use short and long description, depending on distributor.
        - Both should be searched, the short description should remain the one that is shown in tables by default.
    - Part aliases (for digikey: OtherNames)
        - May be considered for resolution during review, but generally not automatically
    - Store URLs for distributor product pages, datasheet (already stored IIRC) and potentially product pictures.

### Category Handling

Digikey and other distributors provide categorization information for us. We cannot use this information directly, however it may still be useful:

#### Part A: General data model and backend considerations

Distributors can each have their own categorization system, and the categories as defined by the distributor can either be more or less useful. Generally, while there will be overlaps between distributors, we cannot assume their categories to be compatible. The goal of this work package will be to introduce a partrack categorization system that maps distributor assigned categories to a common set in partrack, and the other way around. It can be assumed that not considering hierarchy, one partrack category will map to one or more distributor categories, while some distributor categories will not have a mapping to a partrack category, in which case their parent mapping is checked (If available). We will have to allow components to be uncategorized if there is no mapping for them yet.

Thoughts:
- We should figure out a useful default mapping, while also providing the option to override category mapping in the application configuration file. Generally, categories and their mapping should be configurable. Open question: Do we store this configuration data in the database, or is it enough to keep in the application config file?
- To bootstrap our category system, let's have a look at that of digikey:
    - First, let's query the digikey API to find a comlete list of categories:
    - See `jq '.definitions.Categories' doc/distributor/digikey/ProductSearch.json` for the API
    - For the structure of the response: `jq '.definitions.CategoriesResponse' doc/distributor/digikey/ProductSearch.json`
    - There may be a lot of output from the API call, so don't execute it yourself, but write a short script that will take our digikey credentials and download and print the full list. (Just dump the response as json to std, we can go from there. Just use curl and jq for the auth flow)
    - We will have to discuss the standardized partrack parameter set and hierarchy. This will probably be close to the one defined by digikey, with a bunch of specialty categories removed.
    - Then the mapping must be created and stored in the appropriate format

- Categories can be hierarchical. Some distributors may have a "Resistors" and a "Power Resistors" category, where Resistors is the parent category of "Power Resistors". I believe we should also introduce this into the partrack category system.
- As for how categories are reflected in the database. It may not be necessary to implement the category hierarchy in the DB. The server can simply query for a parent category by selecting by it's own ID and that of its children recursively. (Makes sense?). That way only one new category ID column will be required in the database.
- We can assume that a partrack category purpose and their corresponding ID will never change. A category may be deleted from partrack, but category IDs will not be reused. Use an efficient indexable datatype for these first class categories. (Do we really need UUIDs when the maximum expected number of categories can be expected to never exceed 1M?). As such, the system must consider unknown category IDs as uncategorized.
- Distributor supplied categories may be moved onto the distributor listings where they were pulled from and generally become second class objects for reference only. This must be reflected both in the backend in how it is used, as well as in the frontend in how it is presented.
- Categories should define a set of primary parameters, these are the parameters which all distributors should be supplying. For a resistors category, this would for example be the Resistance and Tolerance, while for a Capacitor category the capacitor dielectric, Capacitance, Tolerance and capacitor type may be important. Some parameters like Package (Not packaging, which is a listing attribute!) and category should always be considered primary parameters.

#### Part B: Frontend changes

- As for displaying a components category not much will need to change. The API can probably stay the way it is, returning a category string.
- The user must be able to find parts by category. Imagine they are looking for resistors, they should be given the option to navigate categories hierarchically, and then search within that category. When the user has not selected a specific category to search, the full parts catalog is searched.
- Later, we will implement parameter based searches, which will offer different parameters based on the particular category that the user is looking at. Postpone this for now, but keep it in mind when building the parts search / browser.

## Parameter handling

Currently, parameters are extracted and stored as text with some metadata. We have implemented a system that allows parameter data from different sources to be managed, including user overrides, but the the ways things are right now, parameter data is mostly useful as reference when you're already looking at a specific part.
We need to rethink some of the design descisions around parameters to allow for more versatile use, like the ability to locate parts based on their parameter values. As with categories, parameter values and names are distributor specific, and should first be mapped into a common set of partrack parameters, to allow for filtering with data from different sources. Here we can assume a [0,1] to [0,1] between partrack parameters and distributor parameters. A resistance parameter may require slightly different parsing behavior for each distributor, but it is still a resistance parameter. Not every parameter on digikeys side is tracked by partrack, and similarly not every partrack parameter will be set for every component. Does this assumption make sense?

We will need to figure out a good way to build up and maintain partrack parameters and partrack <=> distributor parameter mappings. Is this a process that we should handle beforehand, or does it make sense to involve the user? I would like to have the basic import process covered, but maybe we can flag unhandled parameters for an admin to go through and create / link appropriate partrack parameter values, until which time the search will not offer filtering for the parameter in question.

How is the parameter configuration stored? Is this something that should live in the DB? (It's getting quite complicated, it probably makes sense to do so)

We may have to implement a few parsers that the user can choose from to deal with free text parameter values and converting them to numerical values, value ranges or not changing them at all. Especially for filtering it may be appropritate the store the converted numerical value of the parameters or parameter ranges in the database for in-database search and filtering. This may require us to extend the parameter table to support such numeric values and their corresponding operations. Similarly, standard parsing options should be able to deal with SI Prefixes and separating units from SI prefixes. Are there packages for this? (Converting GHz to (unit, value correction factor before writing to db) = (UNIT_HERTZ, 1e9), mOhm to (UNIT_OHM, 1e-3), etc.). The parsing configuration may have to be build to be configured per distributor and parameter.

I'm not quite sure on how to approach this to enable a unified representation, please think about possible options to build and manage these mappings.
- We will need to update the part import code to set the appropriate parameter entries based on the mappings.
- Should we allow the user to be involved in the parameter mapping process? Or should unknown / unhandled parameters maybe be collected for an admin to take care of?
- Parameters will become important in locating a suitable part. We will extend the part search that was sketched above to offer parameter filters based on the available set of parameters in the specific category that the user selected. If no cateogry was selected (Global search), only text search is necessary.
- We need to think about how to map parameter values to categories. Should the set of searchable parameters simply be the set of parameters available on the components within the category? Probably not a bad idea, but may have to be cached.

Notes:
- Digikey parameter value structure: (Defintion at: `jq .definitions.ParameterValue doc/distributor/digikey/ProductSearch.json`)

## Advanced part search

Currently we have (at least) two different places where the user can search for parts / import parts, with separate implementations. In the Parts page the user may click on Add Part, and is presented with the options to either create a part manually, or import a part from a distributor. This UI is different from the one which is presented during the BOM review process, in which a user is presented with similar possiblities, except that the implementation is different, it looks a bit different and some values are pre-filled. I would like to build a unified parts search component, which will be available from the parts page as expected, but can also be embedded into modals when the user should select a part out of the catalog.

A non-exhaustive list of requirements for this search interface are as follows:

- Local searches can happen automatically when the user changes parameters, while queries should only be sent to distributors after the user has pressed a button / confirmed the serach operation in some way.
- The default should be to search for local parts
- If the user is not finding what they need, they should have the option to extend their search to the available distributors aswell.
- The search needs a fairly promiment feature to choose a category, potentially navigating through the category hierarchy in multiple steps.
- The search results should be structurally the same for distributor results and local results, with distributor results including information about the local stock aswell (If any). See the distributor search more as a search option (Or maybe separate search button).
- A core feature of the search will be the ability of the user to narrow down the parameter search space to find the speific components that they are looking for. This means the search feature will have to integrate with the parameters and category features, providing an appropritate set of parameters to serach for within each category. This also means that parameter values should be part of the results table.
- This may be something that the user can adjust for themselves, with deviations from the default search parameter set being persistent in the user preferences.
- The component will require some means of pre-configuring certain parameter or search string values, which would make this a very useful UI component to replace the custom search which is currently implemented for the BOM review process. Similarly, part substitution searches will be able to use this feature in the future.
- We need to explore the distributor APIs (Digikey first) to see what information they require for filtered / advanced part searches.
    - There may be differences in the parameter handling between how distributors implement their searches, and partrack implements searches. This discrepancy is generally ok, but still something we want to deal with if possible. For example, I believe it would be a good idea to provide the user with the ability to search for numeric value ranges for things like resistances, while the digikey search (At least in their web interface) allows the user to select every individual available value - individually in a list. These differences may have to be taken into account when setting up the search, and some work will be necessary to investigate the handling of these parameters in the digikey API, and to come back to the example, how our numeric value range is translated to the appropriate list for the digikey API. This may necessitate a two step search process, where the first search is used to retrieve parameter values, and the second search is ran with the filtered parameter values.
    - Deferred: Look at search APIs for other distributors, probably Mouser, Farnell or LCSC.

Note that these interactions with the distributor APIs should be cached in memory if possible! Distributor APIs have ratelimits which may be easily exceeded.

## Advanced DigiKey API integration

The goal is to connect the partrack ordering process to the digikey lists and quote APIs. We will not implement the actual digikey ordering APIs for now, but we should introduce a way for the user to interact with these alternative distributor specific ordering flows, instead of offering the purely unified "Export to table" or "Order through distributor" options.

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

## Cart - manual modifications

The cart view currently offers to change the order quantity for parts which are required for builds. This should be tracked as a manual addition to the cart and reflected in "Section 2: Manual Requests" of the Cart, with the needed count for the included builds representing an additional lower bound on the order quantity.


## Events and query invalidation

Currently the server will send query invalidation notifications within a team only to other users, not the one who triggered the action causing the invalidation. Would it make sense to simplify this behavior and use events for data invalidation on the current user aswell, instead of waiting for the handler to return and then manually performing the invalidation? This way we would also immediately notice invalidation bugs in multi-user environments with just a single client.

## Global navigation / search

# Leftover work items

- Check parts reservation
- Figure out why build is not ready with plenty of local stock
- Parameter management

- Convert partrack search (parameters!) to digikey search parameters

# Others

- Fix resolution of unresolved scans.