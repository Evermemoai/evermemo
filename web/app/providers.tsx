"use client";

import { Toast } from "@heroui/react";

// HeroUI v3 needs no provider; we only mount the global toast region.
export function Providers({ children }: { children: React.ReactNode }) {
  return (
    <>
      {children}
      <Toast.Provider placement="bottom end" />
    </>
  );
}
