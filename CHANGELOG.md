# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **SharePoint share links** in the `url` source. Anonymous
  `*.sharepoint.com` share links are now resolved to direct, Range-capable
  download URLs. Single-file (`:b:`) links resolve automatically; for folder
  (`:f:`) links, name the target file via the URL fragment, e.g.
  `https://tenant.sharepoint.com/:f:/s/Site/<token>#models.ckpt`.
