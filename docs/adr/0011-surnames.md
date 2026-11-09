# ADR 0011: The surname at birth and the surname from a marriage

Status: accepted, 2026-11-11.

## Context

A tree from Gramps keeps every name of a person, each with a type: "Birth Name", "Married Name", "Also Known As". A surname part can also have the origin "Taken", which means that the person took it in marriage. Gramps does not say which name to show. Its manual suggests to record a woman under her maiden name and to add the married name as another name. Real trees mix both ways. Some women have the maiden name as the main name, and others have the married name. Some have only the married name with the type "Birth Name". Without a rule, the app shows one woman under her maiden name and the next under her married name.

## Decision

**The data stays as Gramps keeps it.** Heirfold does not move names around. The rule reads the types of the names, so it gives the same answer whichever name is the main one, and the tree passes to Gramps without a change.

**The app shows the surname that the person carried last, and the surname at birth next to it when it differs.** Relatives, letters and documents of the later life know a woman by the surname from her marriage; the surname at birth links her to her parents. So:

- The shown surname is the surname of the latest name from a marriage: a name of the type "Married Name", or a name without a type whose surname has the origin "Taken". The type of a name decides over the origin of its surname: a person chooses the type in a form and sees it, while the origin is a hidden detail and is often wrong. For example, a name of the type "Birth Name" can have a surname with the origin "Taken". A rule that reads the origin first loses that surname at birth. With several such names it is the one with the latest date, or else the main name if it is from a marriage, or else the last one in the list. Without such a name it is the surname of the main name.
- The surname at birth is the surname of the main name if that name is of the type "Birth Name" or has no type, else of the first such other name. "Also Known As" is no surname at birth.
- The page of a person and the family view write «урожд. Сидорова» («née» in English) under the name. A card of a chart, a list row and a search result write «Иванова (Сидорова)». The search finds a person by both surnames. The list of people orders by the shown surname.
- The given name and the patronymic come from the main name.
- A guest does not see a private name, so a private married name leaves the guest with the main name.

`model.Surnames` holds the rule; the card of a person carries `surname`, `birthSurname` and `married`.

**Editing.** The main name has a type field too. A new name takes the given name and the patronymic of the main name, and its type is the other one of the pair. When a woman has one surname, it is the surname of a husband and not of her father, the form says so and offers one button: the main name becomes the name from the marriage, and a name at birth with the surname of the father, in the form for a woman, is added for a check before saving. The page of people shows such women to an editor as a list, so the tree can be put in order. Heirfold never changes a name by itself: the guess about a surname can be wrong, for example when a woman married a namesake.

## Consequences

- A woman with the maiden name as the main name and a married name as another name shows the married name, with the maiden name in brackets.
- A tree that keeps all women under the maiden name, as the Gramps manual suggests, shows them under the married name as soon as it has a name of the type "Married Name". A tree without such names shows the surname of the main name.
- A woman whose one surname is the surname of a husband gets into the list for a check.
