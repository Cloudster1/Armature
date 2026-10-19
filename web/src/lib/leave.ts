/** Leaves for another site, such as a connected app, where the router has no say. */
export function leave(address: string) {
  window.location.assign(address);
}
