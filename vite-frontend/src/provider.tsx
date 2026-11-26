import * as React from "react";
import { useHref, useNavigate } from "react-router-dom";
import { Toaster } from "react-hot-toast";

import { HeroUIProvider } from "@/shadcn-bridge/heroui/system";
import { ThemeProvider } from "@/components/theme-provider";

export interface ProvidersProps {
  children: React.ReactNode;
}

export function Provider({ children }: ProvidersProps) {
  const navigate = useNavigate();

  return (
    <HeroUIProvider navigate={navigate} useHref={useHref}>
      <ThemeProvider>
        {children}
        <Toaster
          position="top-center"
          toastOptions={{
            duration: 2400,
            style: {
              background: "var(--content1)",
              color: "var(--foreground)",
              border: "1px solid var(--divider)",
              borderRadius: "10px",
              padding: "10px 14px",
              boxShadow:
                "0 8px 24px -4px rgb(0 0 0 / 0.12), 0 2px 6px -1px rgb(0 0 0 / 0.06)",
              fontSize: "14px",
            },
          }}
        />
      </ThemeProvider>
    </HeroUIProvider>
  );
}
