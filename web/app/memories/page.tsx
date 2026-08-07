import { MemoriesTable } from "@/components/memories-table";

export const metadata = { title: "Memories · evermemo" };

export default function MemoriesPage() {
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Memories</h1>
        <p className="text-sm text-muted-foreground">
          Search, store, verify, and manage everything your agents remember
        </p>
      </div>
      <MemoriesTable />
    </div>
  );
}
