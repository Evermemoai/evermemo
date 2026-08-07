"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Chip,
  Label,
  ListBox,
  Pagination,
  SearchField,
  Select,
  Skeleton,
  Table,
} from "@heroui/react";
import { api, type Memory } from "@/lib/api";
import { MemoryFormModal } from "./memory-form-modal";
import { MemoryDetailModal } from "./memory-detail-modal-v3";

export function MemoriesTable() {
  const [memories, setMemories] = useState<Memory[] | null>(null);
  const [query, setQuery] = useState("");
  const [namespace, setNamespace] = useState<string>("all");
  const [formOpen, setFormOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Memory | null>(null);
  const [detailId, setDetailId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const pageSize = 15;

  const ns = namespace === "all" ? "" : namespace;

  const load = useCallback(() => {
    const fetcher = query.trim() ? api.search(query, ns, 100) : api.list(ns, 100);
    fetcher.then(setMemories).catch(() => setMemories([]));
  }, [query, ns]);

  useEffect(() => {
    const t = setTimeout(load, query ? 250 : 0); // debounce typing
    return () => clearTimeout(t);
  }, [load, query]);

  useEffect(() => setPage(1), [query, ns]);

  const totalPages = Math.max(1, Math.ceil((memories?.length ?? 0) / pageSize));
  const pageItems = useMemo(
    () => (memories ?? []).slice((page - 1) * pageSize, page * pageSize),
    [memories, page]
  );

  const namespaces = useMemo(() => {
    const set = new Set((memories ?? []).map((m) => m.namespace));
    set.add("default");
    return ["all", ...Array.from(set).sort()];
  }, [memories]);

  function confidenceColor(c: number) {
    return c >= 0.7 ? "success" : c >= 0.4 ? "warning" : "danger";
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3">
        <SearchField
          aria-label="Search memories"
          className="w-full max-w-xs"
          value={query}
          onChange={setQuery}
        >
          <SearchField.Group>
            <SearchField.SearchIcon />
            <SearchField.Input placeholder="Search memories…" />
            <SearchField.ClearButton />
          </SearchField.Group>
        </SearchField>

        <Select
          aria-label="Namespace"
          className="w-[180px]"
          selectedKey={namespace}
          onSelectionChange={(k) => setNamespace(String(k))}
        >
          <Select.Trigger>
            <Select.Value />
            <Select.Indicator />
          </Select.Trigger>
          <Select.Popover>
            <ListBox>
              {namespaces.map((n) => (
                <ListBox.Item key={n} id={n} textValue={n}>
                  {n === "all" ? "All namespaces" : n}
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              ))}
            </ListBox>
          </Select.Popover>
        </Select>

        <div className="flex-1" />
        <Button onPress={() => { setEditTarget(null); setFormOpen(true); }}>
          + Add memory
        </Button>
      </div>

      {memories === null ? (
        <div className="flex flex-col gap-2">
          {[...Array(5)].map((_, i) => (
            <Skeleton key={i} className="h-12 w-full rounded-lg" />
          ))}
        </div>
      ) : (
        <Table className="[--surface:#232323] [--surface-secondary:#2b2b2b] [--surface-tertiary:#2f2f2f]">
          <Table.ScrollContainer>
            <Table.Content aria-label="Memories" className="min-w-[760px]">
              <Table.Header>
                <Table.Column isRowHeader className="w-full">Content</Table.Column>
                <Table.Column>Agent</Table.Column>
                <Table.Column>Namespace</Table.Column>
                <Table.Column>Tags</Table.Column>
                <Table.Column>Confidence</Table.Column>
                <Table.Column>Created</Table.Column>
                <Table.Column aria-label="Actions" />
              </Table.Header>
              <Table.Body>
                {memories.length === 0 ? (
                  <Table.Row>
                    <Table.Cell className="py-10 text-center text-muted-foreground" colSpan={7}>
                      No memories found. Store one with “+ Add memory”.
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  pageItems.map((m) => (
                    <Table.Row key={m.id}>
                      <Table.Cell className="max-w-[380px]">
                        <button
                          className="block w-full cursor-pointer truncate text-left hover:text-accent"
                          title={m.content}
                          onClick={() => setDetailId(m.id)}
                        >
                          {m.content}
                        </button>
                      </Table.Cell>
                      <Table.Cell>
                        {m.agent ? (
                          <Chip color="accent" size="sm" variant="soft">{m.agent}</Chip>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap">{m.namespace}</Table.Cell>
                      <Table.Cell>
                        <div className="flex max-w-[160px] flex-wrap gap-1">
                          {m.tags?.slice(0, 3).map((t) => (
                            <Chip key={t} size="sm">{t}</Chip>
                          ))}
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        <Chip color={confidenceColor(m.confidence)} size="sm" variant="soft">
                          {Math.round(m.confidence * 100)}%
                        </Chip>
                      </Table.Cell>
                      <Table.Cell className="whitespace-nowrap text-muted-foreground">
                        {new Date(m.created_at).toLocaleDateString()}
                      </Table.Cell>
                      <Table.Cell>
                        <div className="flex gap-1">
                          <Button size="sm" variant="tertiary" onPress={() => setDetailId(m.id)}>
                            View
                          </Button>
                          <Button
                            size="sm"
                            variant="tertiary"
                            onPress={() => { setEditTarget(m); setFormOpen(true); }}
                          >
                            Edit
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))
                )}
              </Table.Body>
            </Table.Content>
          </Table.ScrollContainer>
        </Table>
      )}

      {memories !== null && memories.length > pageSize && (
        <div className="flex items-center justify-between">
          <span className="text-xs text-muted-foreground">
            {memories.length} memories · page {page} of {totalPages}
          </span>
          <Pagination>
            <Pagination.Content>
              <Pagination.Item>
                <Pagination.Previous isDisabled={page === 1} onPress={() => setPage((p) => p - 1)}>
                  <Pagination.PreviousIcon />
                </Pagination.Previous>
              </Pagination.Item>
              <Pagination.Item>
                <Pagination.Next isDisabled={page === totalPages} onPress={() => setPage((p) => p + 1)}>
                  <Pagination.NextIcon />
                </Pagination.Next>
              </Pagination.Item>
            </Pagination.Content>
          </Pagination>
        </div>
      )}

      <MemoryFormModal
        key={editTarget?.id ?? "new"}
        memory={editTarget}
        isOpen={formOpen}
        onOpenChange={setFormOpen}
        onSaved={load}
      />
      <MemoryDetailModal
        memoryId={detailId}
        isOpen={detailId !== null}
        onOpenChange={(open) => !open && setDetailId(null)}
        onChanged={load}
      />
    </div>
  );
}
