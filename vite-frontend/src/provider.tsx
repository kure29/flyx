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
            duration: 2000,
            // Plain toasts; the icon carries success / error.
            style: {
              background: "var(--content1)",
              color: "var(--foreground)",
              border: "1px solid var(--divider)",
              boxShadow: "none",
              fontSize: "14px",
            },
          }}
        />
      </ThemeProvider>
    </HeroUIProvider>
  );
}
