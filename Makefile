dev:
	wails dev

release:
	@if [ -z "$(TAG)" ]; then echo "Usage: make release TAG=v1.2.3"; exit 1; fi
	git tag -a $(TAG) -m "Release $(TAG)"
	git push origin $(TAG)
