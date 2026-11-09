package service

import (
	"cczjVideo/app/installedresources"
	"context"
)

func (a *App) InstalledResources(ctx context.Context) (installedresources.Catalog, error) {
	return runHostRequest(a, ctx, "resources.InstalledResources", a.installedResources.List)
}
func (a *App) InstallResource(ctx context.Context, id string) (installedresources.Entry, error) {
	return runHostRequest(a, ctx, "resources.InstallResource", func(ctx context.Context) (installedresources.Entry, error) {
		return a.installedResources.Install(ctx, id)
	})
}
func (a *App) RemoveResource(ctx context.Context, id string) error {
	_, err := runHostRequest(a, ctx, "resources.RemoveResource", func(ctx context.Context) (struct{}, error) { return struct{}{}, a.installedResources.Remove(ctx, id) })
	return err
}
