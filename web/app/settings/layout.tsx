"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { CentralIcon } from "@central-icons-react/all";

const sections = [
  {
    title: "Account",
    items: [
      { href: "/settings/profile", label: "Profile" },
      { href: "/settings/notifications", label: "Notifications" },
      { href: "/settings/security", label: "Security" },
      { href: "/settings/billing", label: "Billing" },
    ],
  },
  {
    title: "Hub",
    items: [
      { href: "/settings/data", label: "Data" },
      { href: "/settings/maintenance", label: "Maintenance" },
    ],
  },
];

export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();

  return (
    <div className="flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-64 shrink-0 flex-col border-r border-default bg-background">
        <div className="px-5 pb-4 pt-5">
          <Link
            href="/"
            className="inline-flex items-center gap-1.5 rounded-md py-1 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <CentralIcon name={"IconArrowLeft" as never} size={16} join="round" fill="outlined" radius="2" stroke="1.5" />
            Back to app
          </Link>
          <h1 className="mt-3 text-xl font-semibold tracking-tight">Settings</h1>
        </div>

        <nav aria-label="Settings" className="flex flex-1 flex-col gap-6 overflow-y-auto px-3 pb-6 pt-1">
          {sections.map((s) => (
            <div key={s.title} className="flex flex-col gap-0.5">
              <span className="mb-1 px-2.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                {s.title}
              </span>
              {s.items.map((i) => {
                const active = pathname === i.href;
                return (
                  <Link
                    key={i.href}
                    href={i.href}
                    aria-current={active ? "page" : undefined}
                    className={`rounded-lg px-2.5 py-1.5 text-sm transition-colors ${
                      active
                        ? "bg-accent-soft font-medium text-accent-soft-foreground"
                        : "text-muted-foreground hover:bg-default hover:text-foreground"
                    }`}
                  >
                    {i.label}
                  </Link>
                );
              })}
            </div>
          ))}
        </nav>
      </aside>

      <main className="mx-auto min-w-0 w-full max-w-3xl flex-1 px-6 pb-16 pt-10 lg:px-10">
        {children}
      </main>
    </div>
  );
}
