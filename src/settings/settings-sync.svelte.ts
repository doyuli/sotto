import { onMount } from "svelte";
import { ConfigSvc, events, type Settings } from "../mygo";

export function createSettingsSync(notify: (message: string) => void) {
  let settings = $state<Settings | null>(null);
  let pendingSave = Promise.resolve();
  let pendingWrites = 0;
  let version = 0;
  let disposed = false;

  function apply(snapshot: Settings): void {
    settings = snapshot;
    version++;
  }

  async function refresh(): Promise<void> {
    const requestedVersion = version;
    try {
      const snapshot = await ConfigSvc.get();
      if (!disposed && version === requestedVersion) apply(snapshot);
    } catch (err) {
      if (!disposed) notify(`读取设置失败：${String(err)}`);
    }
  }

  function save(): Promise<void> {
    if (!settings) return pendingSave;
    const snapshot = $state.snapshot(settings);
    pendingWrites++;
    version++;
    pendingSave = pendingSave.then(async () => {
      try {
        await ConfigSvc.set(snapshot);
      } catch (err) {
        notify(`保存失败：${String(err)}`);
      } finally {
        pendingWrites--;
      }
      // The final read reconciles backend normalization and rolls back failed writes.
      if (!pendingWrites && !disposed) await refresh();
    });
    return pendingSave;
  }

  onMount(() => {
    const stopUpdates = events.settingsChanged.on((snapshot) => {
      // Keep queued local edits visible until the last write has settled.
      if (!pendingWrites && !disposed) apply(snapshot);
    });
    void refresh();

    return () => {
      disposed = true;
      stopUpdates();
    };
  });

  return {
    get settings() { return settings; },
    save,
    waitForSave: () => pendingSave,
  };
}
