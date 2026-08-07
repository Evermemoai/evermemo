"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Button, Card, Chip, Skeleton, Table } from "@heroui/react";
import { api, type Memory } from "@/lib/api";

export default function OverviewPage() {
  const [health, setHealth] = useState<{ status: string; memories: number } | null>(null);
  const [recent, setRecent] = useState<Memory[] | null>(null);

  useEffect(() => {
    api.health().then(setHealth).catch(() => setHealth({ status: "offline", memories: 0 }));
    api.list("", 5).then(setRecent).catch(() => setRecent([]));
  }, []);

  const namespaces = new Set((recent ?? []).map((m) => m.namespace)).size;
  const agents = new Set((recent ?? []).map((m) => m.agent).filter(Boolean)).size;

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Overview</h1>
          <p className="text-sm text-muted-foreground">Your shared memory hub at a glance</p>
        </div>
        <Button>
          <Link href="/memories">Browse memories</Link>
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <Card>
          <Card.Header className="flex-row items-center justify-between">
            <Card.Description>Total memories</Card.Description>
            <Card.Title className="text-3xl">
              {health ? health.memories : <Skeleton className="h-8 w-16" />}
            </Card.Title>
          </Card.Header>
        </Card>
        <Card>
          <Card.Header className="flex-row items-center justify-between">
            <Card.Description>Hub status</Card.Description>
            <Card.Title>
              {health ? (
                <Chip color={health.status === "ok" ? "success" : "danger"} variant="soft">
                  {health.status === "ok" ? "online" : "offline"}
                </Chip>
              ) : (
                <Skeleton className="h-8 w-20" />
              )}
            </Card.Title>
          </Card.Header>
        </Card>
        <Card>
          <Card.Header className="flex-row items-center justify-between">
            <Card.Description>Active agents (recent)</Card.Description>
            <Card.Title className="text-3xl">{recent ? agents : <Skeleton className="h-8 w-10" />}</Card.Title>
          </Card.Header>
        </Card>
      </div>

      <div className="mt-2 flex flex-col gap-1">
        <h2 className="text-lg font-semibold tracking-tight">Recent memories</h2>
        <p className="text-sm text-muted-foreground">
          Latest knowledge stored by your agents across {namespaces || "…"} namespace(s)
        </p>
      </div>

      <Table>
        <Table.ScrollContainer>
          <Table.Content aria-label="Recent memories">
            <Table.Header>
              <Table.Column isRowHeader className="w-full">Content</Table.Column>
              <Table.Column>Agent</Table.Column>
              <Table.Column>Namespace</Table.Column>
              <Table.Column className="whitespace-nowrap">Created</Table.Column>
            </Table.Header>
            <Table.Body>
              {recent === null ? (
                <Table.Row>
                  <Table.Cell colSpan={4}>
                    <Skeleton className="h-8 w-full rounded-lg" />
                  </Table.Cell>
                </Table.Row>
              ) : recent.length === 0 ? (
                <Table.Row>
                  <Table.Cell className="py-10 text-center text-muted-foreground" colSpan={4}>
                    Nothing stored yet. Add your first memory from the Memories page.
                  </Table.Cell>
                </Table.Row>
              ) : (
                recent.map((m) => (
                  <Table.Row key={m.id}>
                    <Table.Cell className="max-w-0">
                      <span className="block truncate" title={m.content}>{m.content}</span>
                    </Table.Cell>
                    <Table.Cell>
                      {m.agent ? (
                        <Chip color="accent" size="sm" variant="soft">{m.agent}</Chip>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </Table.Cell>
                    <Table.Cell className="whitespace-nowrap">{m.namespace}</Table.Cell>
                    <Table.Cell className="whitespace-nowrap text-muted-foreground">
                      {new Date(m.created_at).toLocaleDateString()}
                    </Table.Cell>
                  </Table.Row>
                ))
              )}
            </Table.Body>
          </Table.Content>
        </Table.ScrollContainer>
      </Table>
    </div>
  );
}
