"use client";

import { useEffect, useMemo, useState } from "react";
import { Chip, Skeleton, Table } from "@heroui/react";
import { api, type Memory } from "@/lib/api";

// Agents page: provenance made visible. Aggregates recent memories by agent.
export default function AgentsPage() {
  const [memories, setMemories] = useState<Memory[] | null>(null);

  useEffect(() => {
    api.list("", 1000).then(setMemories).catch(() => setMemories([]));
  }, []);

  const agents = useMemo(() => {
    const by = new Map<string, Memory[]>();
    for (const m of memories ?? []) {
      const a = m.agent || "(unattributed)";
      by.set(a, [...(by.get(a) ?? []), m]);
    }
    return Array.from(by.entries())
      .map(([agent, mems]) => ({
        agent,
        count: mems.length,
        avgConfidence: mems.reduce((s, m) => s + m.confidence, 0) / mems.length,
        namespaces: Array.from(new Set(mems.map((m) => m.namespace))),
        lastActive: mems.map((m) => m.created_at).sort().at(-1) ?? "",
      }))
      .sort((a, b) => b.count - a.count);
  }, [memories]);

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Agents</h1>
        <p className="text-sm text-muted-foreground">Who is writing what to your shared memory</p>
      </div>

      {memories === null ? (
        <div className="flex flex-col gap-2">
          {[...Array(3)].map((_, i) => (
            <Skeleton key={i} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      ) : (
        <Table className="[--surface:#232323] [--surface-secondary:#2b2b2b] [--surface-tertiary:#2f2f2f]">
          <Table.ScrollContainer>
            <Table.Content aria-label="Agents">
              <Table.Header>
                <Table.Column isRowHeader className="w-full">Agent</Table.Column>
                <Table.Column className="whitespace-nowrap">Memories</Table.Column>
                <Table.Column className="whitespace-nowrap">Avg confidence</Table.Column>
                <Table.Column className="whitespace-nowrap">Namespaces</Table.Column>
                <Table.Column className="whitespace-nowrap">Last active</Table.Column>
              </Table.Header>
              <Table.Body>
                {agents.length === 0 ? (
                  <Table.Row>
                    <Table.Cell className="py-10 text-center text-muted-foreground" colSpan={5}>
                      No memories yet — nothing to attribute.
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  agents.map((a) => (
                    <Table.Row key={a.agent}>
                      <Table.Cell>
                        <Chip color="accent" size="sm" variant="soft">{a.agent}</Chip>
                      </Table.Cell>
                      <Table.Cell>{a.count}</Table.Cell>
                      <Table.Cell>
                        <Chip
                          color={a.avgConfidence >= 0.7 ? "success" : a.avgConfidence >= 0.4 ? "warning" : "danger"}
                          size="sm"
                          variant="soft"
                        >
                          {Math.round(a.avgConfidence * 100)}%
                        </Chip>
                      </Table.Cell>
                      <Table.Cell>
                        <div className="flex max-w-[220px] flex-wrap gap-1">
                          {a.namespaces.map((n) => (
                            <Chip key={n} size="sm">{n}</Chip>
                          ))}
                        </div>
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap text-muted-foreground">
                        {a.lastActive ? new Date(a.lastActive).toLocaleString() : "—"}
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
