"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Tooltip } from "@heroui/react";
import { CentralIcon } from "@central-icons-react/all";

function NavIcon({ name }: { name: string }) {
  return (
    <CentralIcon
      name={name as never}
      size={20}
      join="round"
      fill="filled"
      radius="2"
      stroke="1.5"
    />
  );
}

const nav = [
  { href: "/", label: "Overview", icon: <NavIcon name="IconDashboardMiddle" /> },
  { href: "/memories", label: "Memories", icon: <NavIcon name="IconBrain1" /> },
  { href: "/agents", label: "Agents", icon: <NavIcon name="IconPeople" /> },
  { href: "/namespaces", label: "Namespaces", icon: <NavIcon name="IconFolder1" /> },
  { href: "/settings", label: "Settings", icon: <NavIcon name="IconSettingsGear1" /> },
];

export function BottomNav() {
  const pathname = usePathname();

  return (
    <>
      {/* Bottom fade + blur so content scrolls out cleanly behind the nav */}
      <div
        aria-hidden
        className="pointer-events-none fixed inset-x-0 bottom-0 z-40 h-28 bg-gradient-to-t from-background via-background/70 to-transparent backdrop-blur-[2px] [mask-image:linear-gradient(to_top,black_60%,transparent)]"
      />

      <nav
        aria-label="Primary"
        className="fixed bottom-5 left-1/2 z-50 flex -translate-x-1/2 items-center gap-3 rounded-full border border-default bg-surface/80 px-4 py-2 shadow-lg backdrop-blur-xl"
      >
        <Link href="/" className="mr-2 flex items-center pl-1" aria-label="evermemo home">
          <img src="/logo.svg" alt="" className="h-6 w-6 rounded-md" />
        </Link>

        {nav.map((item) => {
          const active = pathname === item.href;
          return (
            <Tooltip key={item.href} delay={200}>
              <Tooltip.Trigger>
                <Link
                  href={item.href}
                  aria-label={item.label}
                  aria-current={active ? "page" : undefined}
                  className={`flex h-11 w-11 items-center justify-center rounded-full transition-colors ${
                    active
                      ? "bg-accent-soft text-accent-soft-foreground"
                      : "text-muted-foreground hover:bg-default hover:text-foreground"
                  }`}
                >
                  {item.icon}
                </Link>
              </Tooltip.Trigger>
              <Tooltip.Content placement="top">{item.label}</Tooltip.Content>
            </Tooltip>
          );
        })}
      </nav>
    </>
  );
}
