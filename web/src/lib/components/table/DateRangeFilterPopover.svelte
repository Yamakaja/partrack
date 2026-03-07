<script lang="ts">
  import { DateRangePicker } from 'bits-ui';
  import { parseDate } from '@internationalized/date';
  import type { DateRange } from 'bits-ui';
  import { CalendarDays, ChevronLeft, ChevronRight } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';
  import { Button } from '$lib/components/ui/button/index.js';

  let {
    columnLabel,
    isFiltered,
    currentFilter = '',
    onApply,
    onClear,
  }: {
    columnLabel: string;
    isFiltered: boolean;
    currentFilter?: string;
    onApply: (encoded: string) => void;
    onClear: () => void;
  } = $props();

  function filterToRange(filter: string): DateRange {
    if (!filter) return { start: undefined, end: undefined };
    const [from, to] = filter.split('|');
    return {
      start: from ? parseDate(from) : undefined,
      end: to ? parseDate(to) : undefined,
    };
  }

  function rangeToFilter(range: DateRange): string {
    return `${range.start?.toString() ?? ''}|${range.end?.toString() ?? ''}`;
  }

  let open = $state(false);
  let value = $state<DateRange>({ start: undefined, end: undefined });

  function handleOpenChange(v: boolean) {
    if (v) value = filterToRange(currentFilter);
    open = v;
  }

  function apply() {
    const encoded = rangeToFilter(value);
    if (!value.start && !value.end) {
      onClear();
    } else {
      onApply(encoded);
    }
    open = false;
  }

  function clear() {
    value = { start: undefined, end: undefined };
    onClear();
    open = false;
  }
</script>

<DateRangePicker.Root
  bind:value
  {open}
  onOpenChange={handleOpenChange}
  closeOnRangeSelect={false}
  granularity="day"
>
  <DateRangePicker.Trigger
    class={cn(
      'rounded p-0.5 hover:bg-accent transition-colors cursor-pointer',
      isFiltered ? 'text-primary' : 'text-muted-foreground opacity-60 hover:opacity-100',
    )}
    aria-label="Filter {columnLabel} by date range"
  >
    <CalendarDays class="h-3 w-3" />
  </DateRangePicker.Trigger>

  <DateRangePicker.Content
    align="start"
    class="z-50 rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    <DateRangePicker.Calendar>
      {#snippet children({ months, weekdays })}
        {#each months as month}
          <DateRangePicker.Header class="flex items-center justify-between mb-2">
            <DateRangePicker.PrevButton
              class="rounded p-1 hover:bg-accent transition-colors text-muted-foreground hover:text-foreground"
            >
              <ChevronLeft class="h-4 w-4" />
            </DateRangePicker.PrevButton>
            <DateRangePicker.Heading class="text-sm font-medium" />
            <DateRangePicker.NextButton
              class="rounded p-1 hover:bg-accent transition-colors text-muted-foreground hover:text-foreground"
            >
              <ChevronRight class="h-4 w-4" />
            </DateRangePicker.NextButton>
          </DateRangePicker.Header>

          <DateRangePicker.Grid class="w-full border-collapse">
            <DateRangePicker.GridHead>
              <DateRangePicker.GridRow class="flex mb-1">
                {#each weekdays as day}
                  <DateRangePicker.HeadCell
                    class="w-8 text-center text-xs text-muted-foreground font-normal"
                  >
                    {day}
                  </DateRangePicker.HeadCell>
                {/each}
              </DateRangePicker.GridRow>
            </DateRangePicker.GridHead>

            <DateRangePicker.GridBody>
              {#each month.weeks as weekDates}
                <DateRangePicker.GridRow class="flex w-full">
                  {#each weekDates as date}
                    <DateRangePicker.Cell {date} month={month.value} class="relative p-0">
                      <DateRangePicker.Day
                        class={cn(
                          'h-8 w-8 flex items-center justify-center rounded-md text-xs transition-colors',
                          'hover:bg-accent hover:text-accent-foreground',
                          'data-[selected]:bg-primary data-[selected]:text-primary-foreground data-[selected]:hover:bg-primary',
                          'data-[highlighted]:bg-primary/15 data-[highlighted]:rounded-none',
                          'data-[selection-start]:rounded-l-md data-[selection-end]:rounded-r-md',
                          'data-[disabled]:opacity-30 data-[disabled]:pointer-events-none',
                          'data-[outside-month]:text-muted-foreground data-[outside-month]:opacity-40',
                          'data-[unavailable]:line-through data-[unavailable]:opacity-30',
                        )}
                      >
                        {date.day}
                      </DateRangePicker.Day>
                    </DateRangePicker.Cell>
                  {/each}
                </DateRangePicker.GridRow>
              {/each}
            </DateRangePicker.GridBody>
          </DateRangePicker.Grid>
        {/each}

        <div class="flex gap-2 mt-3">
          <Button size="sm" class="flex-1 h-7 text-xs" onclick={apply}>Apply</Button>
          {#if isFiltered}
            <Button variant="outline" size="sm" class="h-7 text-xs" onclick={clear}>
              Clear
            </Button>
          {/if}
        </div>
      {/snippet}
    </DateRangePicker.Calendar>
  </DateRangePicker.Content>
</DateRangePicker.Root>
