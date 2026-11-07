/**
 * FLYX Theme System — Type Definitions
 * =====================================
 * This file defines the contract that every theme package must implement.
 * Theme authors: read README.md first, then implement ThemePackage.
 */

import type React from "react";
import type { ComponentType } from "react";

// ─── CSS Variable Tokens ─────────────────────────────────────────────────────

/**
 * Complete set of CSS variable tokens a theme can define.
 * All values are valid CSS colour strings (hex, rgb, hsl, etc.).
 * A theme does NOT need to provide every token — missing ones fall back
 * to the default theme.
 */
export interface ThemeTokens {
  /* ── base surfaces ─────────────────────── */
  "--background"?: string;
  "--foreground"?: string;
  "--border"?: string;
  "--input"?: string;
  "--ring"?: string;
  "--content1"?: string;
  "--divider"?: string;

  /* ── default (neutral) palette ─────────── */
  "--default-50"?: string;
  "--default-100"?: string;
  "--default-200"?: string;
  "--default-300"?: string;
  "--default-400"?: string;
  "--default-500"?: string;
  "--default-600"?: string;
  "--default-700"?: string;
  "--default-800"?: string;
  "--default-900"?: string;

  /* ── primary ───────────────────────────── */
  "--primary"?: string;
  "--primary-foreground"?: string;
  "--primary-50"?: string;
  "--primary-100"?: string;
  "--primary-200"?: string;
  "--primary-300"?: string;
  "--primary-400"?: string;
  "--primary-500"?: string;
  "--primary-600"?: string;
  "--primary-700"?: string;
  "--primary-800"?: string;
  "--primary-900"?: string;

  /* ── secondary ─────────────────────────── */
  "--secondary"?: string;
  "--secondary-foreground"?: string;
  "--secondary-50"?: string;
  "--secondary-100"?: string;
  "--secondary-200"?: string;
  "--secondary-300"?: string;
  "--secondary-400"?: string;
  "--secondary-500"?: string;
  "--secondary-600"?: string;
  "--secondary-700"?: string;
  "--secondary-800"?: string;
  "--secondary-900"?: string;

  /* ── danger ────────────────────────────── */
  "--danger"?: string;
  "--danger-50"?: string;
  "--danger-100"?: string;
  "--danger-200"?: string;
  "--danger-300"?: string;
  "--danger-400"?: string;
  "--danger-500"?: string;
  "--danger-600"?: string;
  "--danger-700"?: string;
  "--danger-800"?: string;
  "--danger-900"?: string;

  /* ── success ───────────────────────────── */
  "--success"?: string;
  "--success-50"?: string;
  "--success-100"?: string;
  "--success-200"?: string;
  "--success-300"?: string;
  "--success-400"?: string;
  "--success-500"?: string;
  "--success-600"?: string;
  "--success-700"?: string;
  "--success-800"?: string;
  "--success-900"?: string;

  /* ── warning ───────────────────────────── */
  "--warning"?: string;
  "--warning-50"?: string;
  "--warning-100"?: string;
  "--warning-200"?: string;
  "--warning-300"?: string;
  "--warning-400"?: string;
  "--warning-500"?: string;
  "--warning-600"?: string;
  "--warning-700"?: string;
  "--warning-800"?: string;
  "--warning-900"?: string;

  /* ── typography ─────────────────────────── */
  "--font-sans"?: string;
  "--font-mono"?: string;

  /* ── geometry ───────────────────────────── */
  "--radius"?: string;
  "--radius-sm"?: string;
  "--radius-lg"?: string;

  /** Escape hatch: any extra CSS variable */
  [key: `--${string}`]: string | undefined;
}

// ─── Component Keys ──────────────────────────────────────────────────────────

/**
 * All overridable component keys.  These exactly correspond to the exports
 * from `src/shadcn-bridge/heroui/*` and `src/components/*`.
 *
 * A theme only needs to override the components it wants to change.
 * Every other component falls through to the default implementation.
 */
export type ComponentKey =
  // shadcn-bridge/heroui primitives
  | "Button"
  | "Card"
  | "CardHeader"
  | "CardBody"
  | "CardFooter"
  | "Input"
  | "Select"
  | "SelectItem"
  | "Switch"
  | "Checkbox"
  | "Chip"
  | "Modal"
  | "ModalContent"
  | "ModalHeader"
  | "ModalBody"
  | "ModalFooter"
  | "Table"
  | "TableHeader"
  | "TableBody"
  | "TableRow"
  | "TableCell"
  | "TableColumn"
  | "Tabs"
  | "Tab"
  | "Progress"
  | "Spinner"
  | "Divider"
  | "Link"
  | "Dropdown"
  | "DropdownTrigger"
  | "DropdownMenu"
  | "DropdownItem"
  | "Navbar"
  | "NavbarContent"
  | "NavbarItem"
  | "Radio"
  | "RadioGroup"
  | "Accordion"
  | "AccordionItem"
  | "DatePicker"
  | "Alert"
  // app-level components
  | "SearchBar"
  | "BrandLogo"
  | "VersionFooter"
  | "PageWrapper"
  | "PageState"
  | "BatchActionResultModal";

/**
 * All overridable layout keys.
 * Layouts receive `{ children: React.ReactNode }` as props.
 */
export type LayoutKey =
  | "AdminLayout"
  | "H5Layout"
  | "H5SimpleLayout"
  | "DefaultLayout";

/**
 * All overridable page keys.
 * Pages are rendered as route components — they receive no props from the
 * router (params come from React Router hooks).
 */
export type PageKey =
  | "LoginPage"
  | "DashboardPage"
  | "MonitorPage"
  | "ForwardPage"
  | "TunnelPage"
  | "NodePage"
  | "UserPage"
  | "GroupPage"
  | "ProfilePage"
  | "LimitPage"
  | "ConfigPage"
  | "PanelSharingPage"
  | "SettingsPage"
  | "ChangePasswordPage";

// ─── Theme Package ───────────────────────────────────────────────────────────

/**
 * The main interface a theme must export as its default export.
 *
 * Minimal theme (colours only):
 * ```ts
 * const theme: ThemePackage = {
 *   id: "my-theme",
 *   name: "My Theme",
 *   author: "Me",
 *   version: "1.0.0",
 *   tokens: { light: { "--primary": "#ff6600" } },
 * };
 * export default theme;
 * ```
 *
 * Full theme (components + layouts + pages):
 * ```ts
 * import MyButton from "./components/button";
 * import MyAdminLayout from "./layouts/admin";
 * const theme: ThemePackage = {
 *   id: "my-theme",
 *   ...
 *   tokens: { ... },
 *   components: { Button: MyButton },
 *   layouts:    { AdminLayout: MyAdminLayout },
 *   pages:      { DashboardPage: MyDashboard },
 *   css: `body { font-family: "Comic Sans MS" !important; }`,
 *   onActivate: () => console.log("Activated!"),
 * };
 * ```
 */
export interface ThemePackage {
  /** Unique identifier (kebab-case, e.g. "midnight-purple"). */
  id: string;
  /** Human-readable display name. */
  name: string;
  /** Author name or GitHub handle. */
  author: string;
  /** SemVer version string. */
  version: string;
  /** Short description shown in theme picker. */
  description?: string;
  /** Absolute or relative URL to a preview screenshot. */
  preview?: string;

  // ── Styling ────────────────────────────────────────────────────────────────

  /**
   * CSS variable token overrides.  Provide `light`, `dark`, or both.
   * Only the variables you specify will be overridden; all others keep
   * the default values from `globals.css`.
   */
  tokens?: {
    light?: ThemeTokens;
    dark?: ThemeTokens;
  };

  /**
   * Raw CSS string injected into a `<style>` element when this theme is
   * active.  Use this for custom selectors, animations, font-faces, etc.
   * The style element is removed when the theme is deactivated.
   */
  css?: string;

  // ── Component / Layout / Page Overrides ────────────────────────────────────

  /**
   * Map of component overrides.  The replacement component MUST accept the
   * same props interface as the original.  Import types from
   * `@/shadcn-bridge/heroui/*` for reference.
   */
  components?: Partial<Record<ComponentKey, ComponentType<any>>>;

  /**
   * Map of layout overrides.  Each layout receives `{ children }`.
   */
  layouts?: Partial<
    Record<LayoutKey, ComponentType<{ children: React.ReactNode }>>
  >;

  /**
   * Map of page overrides.  Each page is a full route-level component.
   */
  pages?: Partial<Record<PageKey, ComponentType<any>>>;

  // ── Lifecycle ──────────────────────────────────────────────────────────────

  /** Called when this theme becomes the active theme. */
  onActivate?: () => void;
  /** Called when this theme is being replaced by another. */
  onDeactivate?: () => void;
}
