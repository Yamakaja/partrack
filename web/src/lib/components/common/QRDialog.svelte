<script lang="ts">
  import { Dialog } from 'bits-ui';
  import QRCode from 'qrcode';
  import { Button } from '$lib/components/ui/button/index.js';
  import { copyQRImage, downloadQRPng, copyText } from '$lib/utils/qr.js';

  interface Props {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    title: string;
    description: string;
    payload: string;
    filename: string;
  }

  let { open, onOpenChange, title, description, payload, filename }: Props = $props();

  let svg = $state('');
  let imgCopied = $state(false);
  let dataCopied = $state(false);

  $effect(() => {
    if (!open || !payload) return;
    QRCode.toString(payload, { type: 'svg', errorCorrectionLevel: 'M', margin: 2 })
      .then((s) => { svg = s; });
  });

  function handleOpenChange(v: boolean) {
    if (!v) {
      imgCopied = false;
      dataCopied = false;
    }
    onOpenChange(v);
  }
</script>

<Dialog.Root {open} onOpenChange={handleOpenChange}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">{title}</Dialog.Title>
      <Dialog.Description class="text-sm text-muted-foreground mb-4">{description}</Dialog.Description>
      <div class="flex justify-center mb-4">
        {@html svg}
      </div>
      <div class="flex gap-2">
        <Button variant="outline" size="sm" class="flex-1" onclick={async () => {
          await copyQRImage(svg);
          imgCopied = true;
          setTimeout(() => { imgCopied = false; }, 2000);
        }}>
          {imgCopied ? 'Copied!' : 'Copy Image'}
        </Button>
        <Button variant="outline" size="sm" class="flex-1" onclick={() => downloadQRPng(svg, filename)}>
          Download PNG
        </Button>
        <Button variant="outline" size="sm" class="flex-1" onclick={async () => {
          await copyText(payload);
          dataCopied = true;
          setTimeout(() => { dataCopied = false; }, 2000);
        }}>
          {dataCopied ? 'Copied!' : 'Copy Data'}
        </Button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
