// GitHub OAuth round trip. Login and binding both come back to one page,
// /github-callback, so the OAuth App needs a single callback URL. The state
// is remembered in this tab with the flow that started it, so a link
// carrying someone else's code (login CSRF) is ignored.

export type GithubFlow = "login" | "bind";

const STATE_KEY = "flyx_github_state";

/** The callback URL: what the OAuth App's callback URL must be set to. */
export const githubCallbackUrl = () =>
  `${window.location.origin}${import.meta.env.BASE_URL}github-callback`;

/** Remembers the state in the authorize URL and leaves for GitHub. */
export function goToGithub(flow: GithubFlow, authorizeUrl: string) {
  const state = new URL(authorizeUrl).searchParams.get("state") || "";

  try {
    sessionStorage.setItem(STATE_KEY, JSON.stringify({ flow, state }));
  } catch {
    // Without storage the callback is rejected; nothing else to do.
  }
  window.location.href = authorizeUrl;
}

export type GithubCallback =
  | { flow: GithubFlow; code: string; state: string }
  | { flow: GithubFlow | null; error: string };

/** Reads GitHub's answer from the address bar of the callback page. */
export function takeGithubCallback(): GithubCallback {
  const params = new URLSearchParams(window.location.search);
  const code = params.get("code");
  const state = params.get("state");
  let saved: { flow?: GithubFlow; state?: string } = {};

  try {
    saved = JSON.parse(sessionStorage.getItem(STATE_KEY) || "{}");
    sessionStorage.removeItem(STATE_KEY);
  } catch {
    saved = {};
  }
  const flow =
    saved.flow === "bind" || saved.flow === "login" ? saved.flow : null;

  if (params.get("error")) return { flow, error: "已取消 GitHub 授权" };
  if (!code || !state || !flow || state !== saved.state) {
    return { flow, error: "GitHub 授权已失效，请重新操作" };
  }

  return { flow, code, state };
}
