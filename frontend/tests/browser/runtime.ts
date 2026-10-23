// Fixture bindings must use Vite's same runtime module as production adapters;
// a direct /node_modules import creates a second CancelError constructor.
export { CancellablePromise } from '@wailsio/runtime'
