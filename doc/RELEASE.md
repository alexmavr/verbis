# Release process

Verbis follows semantic versioning. At any point in time, we support the current
minor release (`v0.1.x`) and the previous minor release for security updates and
high severity bugfixes (`v0.0.y`)

To create a new release:

### Pre-release checklist
1. Check out the appropriate branch (`main` for the current minor version, `vx.y` for
   supported past minor versions)
2. Ensure the branch is up-to-date with the upstream
3. Ensure the builder machine has an appropriate .builder.env, populated with
   the apple signing key and other required variables
4. Run the latest test suite applicable to the release version


### Release process
5. Update the Makefile VERSION and package.json version parameters to the new version tag
6. Run `make clean` to remove any previous artifacts which may be stale
7. Run `make release` to generate the new release artifacts
8. Commit the updated Makefile to the appropriate branch
9. Create a new git tag `releases/vx.y.z`
10. Upload the DMG and any other applicable artifacts to Github releases, and to
    the releases s3 bucket
11. Push the new commit and tag
