export type UpdateReleaseChannel = "stable" | "dev";

export const UPDATE_CHANNEL_STORAGE_KEY = "update-release-channel";
export const UPDATE_CHANNEL_CHANGED_EVENT = "updateReleaseChannelChanged";

const CHANNEL_STABLE: UpdateReleaseChannel = "stable";
const CHANNEL_DEV: UpdateReleaseChannel = "dev";

const stableVersionPattern = /^\d+(?:\.\d+)+$/;
const testKeywordPattern = /(alpha|beta|rc)/i;

const VERSION_CACHE_TTL_MS = 10 * 60 * 1000;

type ReleaseItem = {
  tag_name?: string;
  draft?: boolean;
};

type LatestVersionCacheEntry = {
  value: string | null;
  expiresAt: number;
};

const latestVersionCache: Record<
  UpdateReleaseChannel,
  LatestVersionCacheEntry
> = {
  stable: { value: null, expiresAt: 0 },
  dev: { value: null, expiresAt: 0 },
};

const normalizeChannel = (
  value: string | null | undefined,
): UpdateReleaseChannel => {
  return value === CHANNEL_DEV ? CHANNEL_DEV : CHANNEL_STABLE;
};

// This fork only publishes stable releases, so the channel is fixed.
export const getUpdateReleaseChannel = (): UpdateReleaseChannel =>
  CHANNEL_STABLE;

export const setUpdateReleaseChannel = (
  channel: UpdateReleaseChannel,
): void => {
  if (typeof window === "undefined") {
    return;
  }

  localStorage.setItem(UPDATE_CHANNEL_STORAGE_KEY, normalizeChannel(channel));
  window.dispatchEvent(new Event(UPDATE_CHANNEL_CHANGED_EVENT));
};

const normalizeTag = (tag: string): string => {
  return tag.trim().replace(/^v/i, "");
};

type ReleaseTagChannel = UpdateReleaseChannel | null;

const releaseChannelFromTag = (tag: string): ReleaseTagChannel => {
  const normalizedTag = normalizeTag(tag).toLowerCase();

  if (!normalizedTag) {
    return null;
  }

  if (stableVersionPattern.test(normalizedTag)) {
    return CHANNEL_STABLE;
  }

  if (testKeywordPattern.test(normalizedTag)) {
    return CHANNEL_DEV;
  }

  return null;
};

type VersionParts = {
  numbers: number[];
  stageRank: number;
  stageNumber: number;
};

const parseVersionParts = (version: string): VersionParts => {
  const normalized = normalizeTag(version).toLowerCase();
  // Only the leading x.y.z is the version: the 1 in "rc.1" is the stage's
  // number, not a fourth version part. Matches the backend's comparison.
  const core = normalized.match(/^\d+(?:\.\d+)*/)?.[0] ?? "";
  const numbers = core
    ? core.split(".").map((item) => Number.parseInt(item, 10))
    : [];

  let stageRank = 0;

  if (normalized.includes("rc")) {
    stageRank = 3;
  } else if (normalized.includes("beta")) {
    stageRank = 2;
  } else if (normalized.includes("alpha")) {
    stageRank = 1;
  } else if (stableVersionPattern.test(normalized)) {
    stageRank = 4;
  }

  const stageNumberMatch = normalized.match(/(?:alpha|beta|rc)[.-]?(\d+)/);
  const stageNumber = stageNumberMatch
    ? Number.parseInt(stageNumberMatch[1], 10)
    : 0;

  return {
    numbers,
    stageRank,
    stageNumber,
  };
};

export const compareVersions = (left: string, right: string): number => {
  const a = parseVersionParts(left);
  const b = parseVersionParts(right);
  const maxLength = Math.max(a.numbers.length, b.numbers.length);

  for (let i = 0; i < maxLength; i += 1) {
    const aValue = a.numbers[i] || 0;
    const bValue = b.numbers[i] || 0;

    if (aValue !== bValue) {
      return aValue - bValue;
    }
  }

  if (a.stageRank !== b.stageRank) {
    return a.stageRank - b.stageRank;
  }

  if (a.stageNumber !== b.stageNumber) {
    return a.stageNumber - b.stageNumber;
  }

  return 0;
};

const repoPathFromUrl = (repoUrl: string): string | null => {
  try {
    const parsed = new URL(repoUrl);
    const segments = parsed.pathname
      .replace(/\.git$/i, "")
      .split("/")
      .filter(Boolean);

    if (segments.length < 2) {
      return null;
    }

    return `${segments[0]}/${segments[1]}`;
  } catch {
    return null;
  }
};

export const getLatestVersionByChannel = async (
  channel: UpdateReleaseChannel,
  repoUrl: string,
): Promise<string | null> => {
  // The online demo has no backend: use the version it was recorded with,
  // as its settings page does. The check is inline so other builds drop it.
  if (import.meta.env.VITE_DEMO === "true") {
    const { demoLatestVersion } = await import("@/demo");

    return demoLatestVersion();
  }

  const normalizedChannel = normalizeChannel(channel);
  const now = Date.now();
  const cached = latestVersionCache[normalizedChannel];

  if (cached.value && cached.expiresAt > now) {
    return cached.value;
  }

  const repoPath = repoPathFromUrl(repoUrl);

  if (!repoPath) {
    return null;
  }

  let releases: ReleaseItem[];

  try {
    const response = await fetch(
      `https://api.github.com/repos/${repoPath}/releases?per_page=50`,
      {
        headers: {
          Accept: "application/vnd.github+json",
        },
      },
    );

    if (!response.ok) {
      return null;
    }

    releases = (await response.json()) as ReleaseItem[];
  } catch {
    // Background update checks also run when GitHub is unreachable.
    return null;
  }

  const candidateTags = releases
    .filter((release) => !release.draft && typeof release.tag_name === "string")
    .map((release) => (release.tag_name || "").trim())
    .filter((tag) => releaseChannelFromTag(tag) === normalizedChannel);

  if (candidateTags.length === 0) {
    return null;
  }

  const latest = candidateTags.sort((a, b) => compareVersions(b, a))[0];

  latestVersionCache[normalizedChannel] = {
    value: latest,
    expiresAt: now + VERSION_CACHE_TTL_MS,
  };

  return latest;
};

export const hasVersionUpdate = (
  currentVersion: string,
  latestVersion: string,
): boolean => {
  return (
    compareVersions(normalizeTag(currentVersion), normalizeTag(latestVersion)) <
    0
  );
};
