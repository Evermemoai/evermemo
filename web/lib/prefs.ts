"use client";

// Local-first preferences: profile, workspace, and team live in localStorage
// until the hub grows user accounts.

export type Profile = { name: string; email: string; title: string };
export type Workspace = { name: string; description: string };
export type Role = "owner" | "admin" | "member";
export type Member = { id: string; email: string; role: Role; addedAt: string };

const read = <T,>(key: string, fallback: T): T => {
  if (typeof window === "undefined") return fallback;
  try {
    const raw = localStorage.getItem(key);
    return raw ? { ...fallback, ...JSON.parse(raw) } : fallback;
  } catch {
    return fallback;
  }
};

const write = (key: string, value: unknown) => localStorage.setItem(key, JSON.stringify(value));

export const getProfile = () => read<Profile>("evermemo.profile", { name: "", email: "", title: "" });
export const saveProfile = (p: Profile) => write("evermemo.profile", p);

export const getWorkspace = () => read<Workspace>("evermemo.workspace", { name: "My workspace", description: "" });
export const saveWorkspace = (w: Workspace) => write("evermemo.workspace", w);

export const getMembers = (): Member[] => read<Member[]>("evermemo.members", []);
export const saveMembers = (m: Member[]) => write("evermemo.members", m);
