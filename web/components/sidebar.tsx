"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Chip, Separator, Tooltip } from "@heroui/react";
import { CentralIcon } from "@central-icons-react/all";
import { api } from "@/lib/api";

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

export function Sidebar() {
  const pathname = usePathname();
  const [collapsed, setCollapsed] = useState(false);
  const [health, setHealth] = useState<{ status: string; memories: number } | null>(null);

  useEffect(() => {
    setCollapsed(localStorage.getItem("evermemo.sidebar") === "collapsed");
  }, []);

  useEffect(() => {
    const load = () => api.health().then(setHealth).catch(() => setHealth(null));
    load();
    const t = setInterval(load, 30_000);
    return () => clearInterval(t);
  }, []);

  const toggle = () => {
    setCollapsed((c) => {
      localStorage.setItem("evermemo.sidebar", c ? "expanded" : "collapsed");
      return !c;
    });
  };

  const item = (href: string, label: string, icon: React.ReactNode) => {
    const active = pathname === href;
    const link = (
      <Link
        href={href}
        aria-label={label}
        aria-current={active ? "page" : undefined}
        className={`flex h-10 items-center rounded-lg transition-colors ${
          collapsed ? "w-10 justify-center" : "gap-3 px-3"
        } ${
          active
            ? "bg-accent-soft text-accent-soft-foreground"
            : "text-muted-foreground hover:bg-default hover:text-foreground"
        }`}
      >
        {icon}
        {!collapsed && <span className="text-sm font-medium">{label}</span>}
      </Link>
    );
    if (!collapsed) return <span key={href}>{link}</span>;
    return (
      <Tooltip key={href} delay={200}>
        <Tooltip.Trigger>{link}</Tooltip.Trigger>
        <Tooltip.Content placement="right">{label}</Tooltip.Content>
      </Tooltip>
    );
  };

  return (
    <aside
      className={`sticky top-0 flex h-screen shrink-0 flex-col border-r border-default bg-background transition-[width] duration-200 ${
        collapsed ? "w-16" : "w-60"
      }`}
    >
      <div className={`flex items-center py-5 ${collapsed ? "justify-center" : "gap-2.5 px-5"}`}>
        <img src="/logo.svg" alt="evermemo logo" className="h-7 w-7 rounded-lg" />
        {!collapsed && <span className="text-lg font-semibold tracking-tight">evermemo</span>}
      </div>
      <Separator />

      <nav aria-label="Primary" className={`flex flex-1 flex-col gap-1 py-3 ${collapsed ? "items-center px-0" : "px-3"}`}>
        {nav.map((n) => item(n.href, n.label, n.icon))}
      </nav>

      <div className={`flex flex-col gap-3 pb-4 ${collapsed ? "items-center px-0" : "px-4"}`}>
        {health && (
          collapsed ? (
            <Tooltip delay={200}>
              <Tooltip.Trigger>
                <span
                  aria-label={`Hub ${health.status === "ok" ? "online" : "offline"}`}
                  className={`h-2.5 w-2.5 rounded-full ${health.status === "ok" ? "bg-success" : "bg-danger"}`}
                />
              </Tooltip.Trigger>
              <Tooltip.Content placement="right">
                {health.status === "ok" ? `online · ${health.memories} memories` : "offline"}
              </Tooltip.Content>
            </Tooltip>
          ) : (
            <div className="flex items-center gap-2">
              <Chip color={health.status === "ok" ? "success" : "danger"} size="sm" variant="soft">
                {health.status === "ok" ? "online" : "offline"}
              </Chip>
              <span className="text-xs text-muted-foreground">{health.memories} memories</span>
            </div>
          )
        )}

        <Tooltip delay={200}>
          <Tooltip.Trigger>
            <button
              type="button"
              onClick={toggle}
              aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
              className={`flex h-9 items-center rounded-lg text-muted-foreground transition-colors hover:bg-default hover:text-foreground ${
                collapsed ? "w-9 justify-center" : "gap-3 self-start px-2.5"
              }`}
            >
              <NavIcon name={collapsed ? "IconChevronDoubleRight" : "IconChevronDoubleLeft"} />
              {!collapsed && <span className="text-sm">Collapse</span>}
            </button>
          </Tooltip.Trigger>
          <Tooltip.Content placement="right">
            {collapsed ? "Expand" : "Collapse"}
          </Tooltip.Content>
        </Tooltip>
      </div>
    </aside>
  );
}
