const controllers = new Set<AbortController>();
export function scopedController() {
  const controller = new AbortController();
  controllers.add(controller);
  controller.signal.addEventListener(
    "abort",
    () => controllers.delete(controller),
    { once: true },
  );
  return controller;
}
export function scopedSignal() {
  return scopedController().signal;
}
export function abortTenantRequests() {
  for (const controller of controllers) controller.abort();
  controllers.clear();
}
