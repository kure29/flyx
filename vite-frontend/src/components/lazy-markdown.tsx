import { lazy, Suspense } from "react";

const Markdown = lazy(() => import("@/components/markdown"));

/** Markdown, rendered once its parser loads; plain text until then. */
export function LazyMarkdown({ children }: { children: string }) {
  return (
    <Suspense fallback={<p className="whitespace-pre-wrap">{children}</p>}>
      <Markdown>{children}</Markdown>
    </Suspense>
  );
}
