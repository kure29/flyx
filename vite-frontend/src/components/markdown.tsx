import ReactMarkdown from "react-markdown";
import rehypeSanitize from "rehype-sanitize";
import remarkGfm from "remark-gfm";

/**
 * Announcement text as Markdown (GitHub flavour, HTML sanitized). Loaded on
 * demand through LazyMarkdown: most visits have no announcement, so the
 * Markdown parser stays out of the page.
 */
export default function Markdown({ children }: { children: string }) {
  return (
    <ReactMarkdown
      components={{
        p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
        a: ({ children, href }) => (
          <a
            className="underline underline-offset-2 hover:text-default-500"
            href={href}
            rel="noopener noreferrer"
            target="_blank"
          >
            {children}
          </a>
        ),
        ul: ({ children }) => (
          <ul className="mb-2 list-disc space-y-1 pl-5 last:mb-0">
            {children}
          </ul>
        ),
        ol: ({ children }) => (
          <ol className="mb-2 list-decimal space-y-1 pl-5 last:mb-0">
            {children}
          </ol>
        ),
        code: ({ children }) => (
          <code className="rounded bg-default-100 px-1 py-0.5 font-mono text-[0.92em]">
            {children}
          </code>
        ),
        pre: ({ children }) => (
          <pre className="mb-2 overflow-x-auto rounded-md bg-default-100 p-2.5 font-mono text-xs leading-relaxed">
            {children}
          </pre>
        ),
        blockquote: ({ children }) => (
          <blockquote className="mb-2 border-l-2 border-divider pl-3 text-default-500">
            {children}
          </blockquote>
        ),
        h1: ({ children }) => (
          <p className="mb-2 font-semibold last:mb-0">{children}</p>
        ),
        h2: ({ children }) => (
          <p className="mb-2 font-semibold last:mb-0">{children}</p>
        ),
        h3: ({ children }) => (
          <p className="mb-2 font-semibold last:mb-0">{children}</p>
        ),
      }}
      rehypePlugins={[rehypeSanitize]}
      remarkPlugins={[remarkGfm]}
    >
      {children}
    </ReactMarkdown>
  );
}
