import { useState } from "react";
import { useCredentials, useUpdateCredential, useDeleteCredential } from "@/features/useCredentials";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Key, Pencil, Trash2, Check, X } from "lucide-react";
import { formatDistanceToNow } from "date-fns";

export function SavedLoginsPage() {
  const { data: credentials, isLoading } = useCredentials();
  const updateCredential = useUpdateCredential();
  const deleteCredential = useDeleteCredential();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editLabel, setEditLabel] = useState("");
  const [deleteId, setDeleteId] = useState<string | null>(null);

  const handleStartEdit = (id: string, currentLabel: string) => {
    setEditingId(id);
    setEditLabel(currentLabel);
  };

  const handleSaveEdit = () => {
    if (editingId && editLabel.trim()) {
      updateCredential.mutate({ id: editingId, label: editLabel.trim() });
    }
    setEditingId(null);
  };

  const handleCancelEdit = () => {
    setEditingId(null);
    setEditLabel("");
  };

  const handleDelete = () => {
    if (deleteId) {
      deleteCredential.mutate(deleteId);
      setDeleteId(null);
    }
  };

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Saved Logins</h1>
        <p className="text-muted-foreground">
          Saved logins are encrypted and reused to sign the agent's browser into a site for you.
          They are wiped from the agent after each job.
        </p>
      </div>

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Label</TableHead>
                <TableHead>Origin</TableHead>
                <TableHead>Last Used</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  </TableRow>
                ))
              ) : !credentials || credentials.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-muted-foreground">
                    <Key className="mx-auto h-8 w-8 mb-2" />
                    No saved logins yet. They are created from a live VNC browser session.
                  </TableCell>
                </TableRow>
              ) : (
                credentials?.map((cred) => (
                  <TableRow key={cred.id}>
                    <TableCell className="font-medium">
                      {editingId === cred.id ? (
                        <div className="flex items-center gap-1">
                          <Input
                            value={editLabel}
                            onChange={(e) => setEditLabel(e.target.value)}
                            className="h-8 w-40"
                            autoFocus
                          />
                          <Button variant="ghost" size="icon" onClick={handleSaveEdit} className="h-8 w-8">
                            <Check className="h-4 w-4" />
                          </Button>
                          <Button variant="ghost" size="icon" onClick={handleCancelEdit} className="h-8 w-8">
                            <X className="h-4 w-4" />
                          </Button>
                        </div>
                      ) : (
                        cred.label
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{cred.origin}</TableCell>
                    <TableCell className="text-muted-foreground text-sm">
                      {cred.last_used_at
                        ? formatDistanceToNow(new Date(cred.last_used_at), { addSuffix: true })
                        : "Never"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-1">
                        <Button variant="ghost" size="icon" onClick={() => handleStartEdit(cred.id, cred.label)}>
                          <Pencil className="h-4 w-4" />
                        </Button>
                        <Button variant="ghost" size="icon" onClick={() => setDeleteId(cred.id)}>
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <AlertDialog open={!!deleteId} onOpenChange={(open) => !open && setDeleteId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete saved login?</AlertDialogTitle>
            <AlertDialogDescription>
              This cannot be undone. You'll need to save it again from a live VNC session.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-destructive">
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
