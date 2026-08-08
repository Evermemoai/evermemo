"use client";

import { usePathname } from "next/navigation";
import { Sidebar } from "@/components/sidebar";

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  // Settings is a full-page takeover with its own sidebar.
  if (pathname.startsWith("/settings")) {
    return <>{children}</>;
  }

  return (
    <div className="flex min-h-screen">
      <Sidebar />
      <main className="mx-auto min-w-0 w-full max-w-6xl flex-1 px-6 pb-16 pt-8 lg:px-8">
        {children}
      </main>
    </div>
  );
}
