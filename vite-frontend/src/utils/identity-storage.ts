// Keep existing preferences and let a previous frontend build read changes
// made during the transition. Callers retain their existing storage error handling.
const legacyKey = (key: string) => key.replace(/^flyx([:_])/, "flvx$1");

export function getStoredValue(storage: Storage, key: string): string | null {
  const value = storage.getItem(key);

  if (value !== null || legacyKey(key) === key) return value;

  const legacy = storage.getItem(legacyKey(key));

  if (legacy !== null) {
    try {
      storage.setItem(key, legacy);
    } catch {
      // Reading a saved preference must work even when storage is full.
    }
  }

  return legacy;
}

export function setStoredValue(storage: Storage, key: string, value: string) {
  storage.setItem(key, value);
  if (legacyKey(key) !== key) storage.setItem(legacyKey(key), value);
}

export function removeStoredValue(storage: Storage, key: string) {
  storage.removeItem(key);
  if (legacyKey(key) !== key) storage.removeItem(legacyKey(key));
}
