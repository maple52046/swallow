/**
 * The sections of a Server's Activity tab that other views can deep-link to.
 *
 * The Server list's contextual icon opens the section that records the Server's current work or
 * problem (see `serverContextAction`), and `ServerActivityTab` renders these ids on its sections
 * and scrolls the hashed one into view. Both sides read this one map so a renamed section cannot
 * silently break the list's links. The ids end up in shareable URLs (bookmarks, copied links), so
 * renaming one breaks those too.
 */
export type ServerActivitySection = 'session' | 'provisioning-tasks' | 'provider-events' | 'related-operations'

/** DOM ids (and URL hashes, without `#`) of the Activity tab sections. */
export const SERVER_ACTIVITY_SECTION_IDS: Readonly<Record<ServerActivitySection, string>> = {
  session: 'activity-session',
  'provisioning-tasks': 'activity-provisioning-tasks',
  'provider-events': 'activity-provider-events',
  'related-operations': 'activity-related-operations',
}

/**
 * Section headings, shared by the Activity tab and the list icon tooltips that name where a link
 * lands, so the two always read the same.
 */
export const SERVER_ACTIVITY_SECTION_TITLES: Readonly<Record<ServerActivitySection, string>> = {
  session: 'Current browser session',
  'provisioning-tasks': 'Provisioning tasks',
  'provider-events': 'Provider events',
  'related-operations': 'Related Operations',
}

/**
 * Site-unscoped path to one Activity section. Callers pass it through `scopedHref`, which keeps
 * the hash after the `site` query (`/servers/srv-1/activity?site=a#activity-provider-events`).
 */
export function serverActivityPath(serverId: string, section: ServerActivitySection): string {
  return `/servers/${serverId}/activity#${SERVER_ACTIVITY_SECTION_IDS[section]}`
}
