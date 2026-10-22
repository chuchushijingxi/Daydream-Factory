import { useState } from 'react';
import { teamClient } from '../../../api/team-client';
import { Button } from '../../../components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  DialogFooter,
} from '../../../components/ui/dialog';
import { useCatalogText } from '../../../contexts/useCatalogText';
import { Feedback, Field, useRead } from '../controls';
export function TeamFilter({ value, onChange }: { value: string; onChange: (id: string) => void }) {
  const { c } = useCatalogText(),
    [open, setOpen] = useState(false),
    [search, setSearch] = useState(''),
    [query, setQuery] = useState(''),
    [page, setPage] = useState(1),
    [selected, setSelected] = useState<{ id: string; name: string } | null>(null);
  const read = useRead((signal) => teamClient.teams(query, page, signal), [query, page], open);
  return (
    <>
      <Button
        variant="outline"
        type="button"
        aria-label={c('团队筛选')}
        onClick={() => setOpen(true)}
      >
        {value ? (selected?.id === value ? selected.name : c('已选择团队')) : c('全部空间')}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent variant="picker">
          <DialogTitle>{c('按团队筛选')}</DialogTitle>
          <DialogDescription>
            {c('选择要查看用量的团队，个人空间记录仍保留在全部空间中。')}
          </DialogDescription>
          <form
            className="team-form"
            onSubmit={(event) => {
              event.preventDefault();
              event.stopPropagation();
              setQuery(search.trim());
              setPage(1);
              read.refresh();
            }}
          >
            <Field label={c('搜索团队')}>
              <input value={search} onChange={(event) => setSearch(event.target.value)} />
            </Field>
            <Button type="submit" variant="outline">
              {c('查询')}
            </Button>
          </form>
          <Feedback
            busy={read.busy}
            error={read.error}
            empty={!read.value?.items.length}
            retry={read.refresh}
          />
          <div className="team-user-picker">
            {read.value?.items.map((team) => (
              <button
                type="button"
                className="team-user-option"
                key={team.id}
                aria-pressed={value === team.id}
                onClick={() => {
                  setSelected({ id: team.id, name: team.name });
                  onChange(team.id);
                  setOpen(false);
                }}
              >
                {team.name}
              </button>
            ))}
          </div>
          {read.value && (
            <div className="op-pagination">
              <Button
                variant="ghost"
                disabled={page <= 1 || read.busy}
                onClick={() => setPage((old) => old - 1)}
              >
                {c('上一页')}
              </Button>
              <span>{c('第 {page} 页', { page })}</span>
              <Button
                variant="ghost"
                disabled={read.busy || read.value.page * read.value.pageSize >= read.value.total}
                onClick={() => setPage((old) => old + 1)}
              >
                {c('下一页')}
              </Button>
            </div>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setSelected(null);
                onChange('');
                setOpen(false);
              }}
            >
              {c('全部空间')}
            </Button>
            <Button variant="outline" onClick={() => setOpen(false)}>
              {c('关闭')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
