import React from "react";

/*
 * Page and list wrappers. They used to animate (framer-motion); the
 * minimal design renders pages at once, so these are plain elements kept
 * for their call sites.
 */

export const AnimatedPage = ({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) => <div className={className}>{children}</div>;

export const StaggerList = ({
  children,
  className,
  as: Component = "div",
}: {
  children: React.ReactNode;
  className?: string;
  as?: "div" | "ul" | "tbody";
}) => <Component className={className}>{children}</Component>;

export const StaggerItem = ({
  children,
  className,
  as: Component = "div",
}: {
  children: React.ReactNode;
  className?: string;
  as?: "div" | "li" | "tr";
}) => <Component className={className}>{children}</Component>;
