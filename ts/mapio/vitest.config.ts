import { defineConfig } from 'vitest/config';

export default defineConfig({
  resolve: { conditions: ['auto-pigeon-source'] },
  test: { setupFiles: ['test/support/contract.ts'] },
});
