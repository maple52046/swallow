/**
 * Shared copy for a value that keeps its place on a shared page while the feature
 * that produces it is still in development (see `ExperimentalFeature`).
 *
 * Release builds show this instead of an empty or "unknown" value, so an operator
 * reads "not offered yet" rather than "no data" or a failure. Keep one wording so
 * every placeholder (badges, metric cards, filters, detail facts) reads the same.
 */
export const NOT_AVAILABLE_IN_RELEASE = 'Not available in this release'
