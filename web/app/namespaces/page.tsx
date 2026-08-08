"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { Button, Chip, Skeleton, Table } from "@heroui/react";
import { api, type Memory } from "@/lib/api";

// Namespaces page: how memory is partitioned across projects/teams/agents.
export default function NamespacesPage() {
  const [memories, setMemories] = useState<Memory[] | null>(null);

  useEffect(() => {
    api.list("", 1000).then(setMemories).catch(() => setMemories([]));
  }, []);

  const namespaces = useMemo(() => {
    const by = new Map<string, Memory[]>();
    for (const m of memories ?? []) {
      by.set(m.namespace, [...(by.get(m.namespace) ?? []), m]);
    }
    return Array.from(by.entries())
      .map(([ns, mems]) => ({
        ns,
        count: mems.length,
        agents: Array.from(new Set(mems.map((m) => m.agent).filter(Boolean))) as string[],
        lastUpdated: mems.map((m) => m.updated_at).sort().at(-1) ?? "",
      }))
      .sort((a, b) => b.count - a.count);
  }, [memories]);

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Namespaces</h1>
        <p className="text-sm text-muted-foreground">Memory partitions per project, team, or agent</p>
      </div>

      {memories === null ? (
        <div className="flex flex-col gap-2">
          {[...Array(3)].map((_, i) => (
            <Skeleton key={i} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      ) : (
        <Table>
          <Table.ScrollContainer>
            <Table.Content aria-label="Namespaces">
              <Table.Header>
                <Table.Column isRowHeader className="w-full">Namespace</Table.Column>
                <Table.Column className="whitespace-nowrap">Memories</Table.Column>
                <Table.Column className="whitespace-nowrap">Agents</Table.Column>
                <Table.Column className="whitespace-nowrap">Last updated</Table.Column>
                <Table.Column aria-label="Actions" />
              </Table.Header>
              <Table.Body>
                {namespaces.length === 0 ? (
                  <Table.Row>
                    <Table.Cell className="py-10 text-center text-muted-foreground" colSpan={5}>
                      No memories yet — add one and the default namespace appears here.
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  namespaces.map((n) => (
                    <Table.Row key={n.ns}>
                      <Table.Cell className="font-medium">{n.ns}</Table.Cell>
                      <Table.Cell>{n.count}</Table.Cell>
                      <Table.Cell>
                        <div className="flex max-w-[240px] flex-wrap gap-1">
                          {n.agents.length ? (
                            n.agents.map((a) => (
                              <Chip key={a} color="accent" size="sm" variant="soft">{a}</Chip>
                            ))
                          ) : (
                            <span className="text-muted-foreground">—</span>
                          )}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap text-muted-foreground">
                        {n.lastUpdated ? new Date(n.lastUpdated).toLocaleString() : "—"}
                      </Table.Cell>
                      <Table.Cell>
                        <Button size="sm" variant="tertiary">
                          <Link href="/memories">Browse</Link>
                        </Button>
                      </Table.Cell>
                    </Table.Row>
                  ))
                )}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>
      )}
    </div>
  );
}
