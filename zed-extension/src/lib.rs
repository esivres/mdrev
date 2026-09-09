use std::fs;

use zed_extension_api::{self as zed, settings::LspSettings, Result};

struct MdrevExtension {
    cached_binary_path: Option<String>,
}

impl MdrevExtension {
    /// Resolution order: an explicit path from settings, then a binary already
    /// on PATH (installed with brew, scoop or `go install`), and only then a
    /// download — so a user who manages mdrev themselves never gets a second,
    /// divergent copy.
    ///
    /// The cache is consulted last, immediately before downloading. Checking it
    /// first would pin whatever was downloaded once for the rest of the
    /// session, silently ignoring a path the user adds to their settings or a
    /// binary they install afterwards.
    fn binary_path(
        &mut self,
        language_server_id: &zed::LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<String> {
        if let Ok(settings) = LspSettings::for_worktree(language_server_id.as_ref(), worktree) {
            if let Some(binary) = settings.binary {
                if let Some(path) = binary.path {
                    return Ok(path);
                }
            }
        }

        if let Some(path) = worktree.which("mdrev") {
            return Ok(path);
        }

        if let Some(path) = &self.cached_binary_path {
            if fs::metadata(path).is_ok_and(|stat| stat.is_file()) {
                return Ok(path.clone());
            }
        }

        let path = self.download(language_server_id)?;
        self.cached_binary_path = Some(path.clone());
        Ok(path)
    }

    fn download(&self, language_server_id: &zed::LanguageServerId) -> Result<String> {
        zed::set_language_server_installation_status(
            language_server_id,
            &zed::LanguageServerInstallationStatus::CheckingForUpdate,
        );
        let release = zed::latest_github_release(
            "esivres/mdrev",
            zed::GithubReleaseOptions {
                require_assets: true,
                pre_release: false,
            },
        )
        .map_err(|e| format!("mdrev is not on your PATH and no release could be fetched: {e}"))?;

        let (platform, arch) = zed::current_platform();
        let os = match platform {
            zed::Os::Linux => "linux",
            zed::Os::Mac => "darwin",
            zed::Os::Windows => "windows",
        };
        let arch = match arch {
            zed::Architecture::X8664 => "amd64",
            zed::Architecture::Aarch64 => "arm64",
            unsupported => return Err(format!("unsupported architecture {unsupported:?}")),
        };

        // Match the archive extension too: releases carry checksums, and may
        // later carry packages or signatures, any of which would otherwise be
        // picked up and handed to the archive extractor.
        let (extension, file_type) = if matches!(platform, zed::Os::Windows) {
            (".zip", zed::DownloadedFileType::Zip)
        } else {
            (".tar.gz", zed::DownloadedFileType::GzipTar)
        };
        let suffix = format!("_{os}_{arch}{extension}");
        let asset = release
            .assets
            .iter()
            .find(|asset| asset.name.ends_with(&suffix))
            .ok_or_else(|| {
                format!(
                    "release {} has no asset for {os}/{arch}",
                    release.version
                )
            })?;

        let version_dir = format!("mdrev-{}", release.version);
        let binary_name = if matches!(platform, zed::Os::Windows) {
            "mdrev.exe"
        } else {
            "mdrev"
        };
        let binary_path = format!("{version_dir}/{binary_name}");

        if !fs::metadata(&binary_path).is_ok_and(|stat| stat.is_file()) {
            zed::set_language_server_installation_status(
                language_server_id,
                &zed::LanguageServerInstallationStatus::Downloading,
            );
            zed::download_file(&asset.download_url, &version_dir, file_type)
                .map_err(|e| format!("failed to download mdrev: {e}"))?;

            // Drop the copies left by earlier versions.
            if let Ok(entries) = fs::read_dir(".") {
                for entry in entries.flatten() {
                    let name = entry.file_name();
                    let name = name.to_string_lossy();
                    if name.starts_with("mdrev-") && name != version_dir.as_str() {
                        fs::remove_dir_all(entry.path()).ok();
                    }
                }
            }
        }

        // Outside the guard above: a download that succeeded while this failed
        // would otherwise leave a file that exists but cannot be run, and every
        // later attempt would take the early-out and spawn it again.
        zed::make_file_executable(&binary_path)?;
        zed::set_language_server_installation_status(
            language_server_id,
            &zed::LanguageServerInstallationStatus::None,
        );

        Ok(binary_path)
    }
}

impl zed::Extension for MdrevExtension {
    fn new() -> Self {
        Self {
            cached_binary_path: None,
        }
    }

    fn language_server_command(
        &mut self,
        language_server_id: &zed::LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<zed::Command> {
        // Honour arguments from settings: the CLI's own setup writes a binary
        // block containing them, and ignoring it would make that setting a lie.
        let args = LspSettings::for_worktree(language_server_id.as_ref(), worktree)
            .ok()
            .and_then(|settings| settings.binary)
            .and_then(|binary| binary.arguments)
            .unwrap_or_else(|| vec!["lsp".to_string()]);

        Ok(zed::Command {
            command: self.binary_path(language_server_id, worktree)?,
            args,
            // The server resolves sidecars relative to the document and reads
            // git config for the comment author, so it needs the real
            // environment rather than an empty one.
            env: worktree.shell_env(),
        })
    }
}

zed::register_extension!(MdrevExtension);
