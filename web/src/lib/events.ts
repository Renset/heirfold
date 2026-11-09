// The order of the events of a life. The server uses the same rule
// (OrderEvents in internal/store/view.go). The page needs it to join the
// events of a person with the events of the families, such as a wedding.

const last = Number.MAX_SAFE_INTEGER;

/** A birth stands first and a death last among events of the same time. A burial follows the death. */
function rank(type: string): number {
  switch (type) {
    case "Birth":
      return -1;
    case "Death":
      return 1;
    case "Burial":
    case "Cremation":
      return 2;
    default:
      return 0;
  }
}

/**
 * Puts events in the order of a life. A date that names only the year or the
 * month stands at the start of that time, but a death and a burial stand at
 * its end. An event without a date keeps its place after the event that it
 * followed. A birth without a date goes first and a death without a date last.
 */
export function orderEvents<T>(items: T[], eventOf: (item: T) => { type: string; sort?: number }): T[] {
  let previous = -last + 1;
  let previousRank = 0;
  const keyed = items.map((item, index) => {
    const event = eventOf(item);
    let order = rank(event.type);
    let key = event.sort ?? 0;
    if (key !== 0) {
      if (order > 0) {
        // The end of the year or of the month.
        if (Math.floor(key / 100) % 100 === 0) {
          key += 1300;
        }
        if (key % 100 === 0) {
          key += 32;
        }
      }
      previous = key;
      previousRank = order;
    } else if (order < 0) {
      key = -last;
    } else if (order > 0) {
      key = last;
    } else {
      key = previous;
      order = previousRank;
    }
    return { item, key, order, index };
  });
  keyed.sort((a, b) => a.key - b.key || a.order - b.order || a.index - b.index);
  return keyed.map((entry) => entry.item);
}
