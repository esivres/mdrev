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
    fn binary_path(
        &mut self,
        language_server_id: &zed::LanguageServerId,
        worktree: &zed::Worktree,
    ) -> Result<String> {
        if let Some(path) = &self.cached_binary_path {
            if fs::metadata(path).is_ok_and(|stat| stat.is_file()) {
                return Ok(path.clone());
            }
        }

        if let Ok(settings) = LspSettings::for_worktree(language_server_id.as_ref(), worktree) {
            if let Some(binary) = settings.binary {
                if let Some(path) = binary.path {
                    self.cached_binary_path = Some(path.clone());
                    return Ok(path);
                }
            }
        }

        if let Some(path) = worktree.which("mdrev") {
            self.cached_binary_path = Some(path.clone());
            return Ok(path);
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
        )?;

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

        // Matching on the suffix rather than the full name keeps this working
        // whatever the release archives are named after the version.
        let suffix = format!("_{os}_{arch}.");
        let asset = release
            .assets
            .iter()
            .find(|asset| asset.name.contains(&suffix))
            .ok_or_else(|| format!("no release asset for {os}/{arch}"))?;

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
            let file_type = if asset.name.ends_with(".zip") {
                zed::DownloadedFileType::Zip
            } else {
                zed::DownloadedFileType::GzipTar
            };
            zed::download_file(&asset.download_url, &version_dir, file_type)
                .map_err(|e| format!("failed to download mdrev: {e}"))?;
            zed::make_file_executable(&binary_path)?;

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
        Ok(zed::Command {
            command: self.binary_path(language_server_id, worktree)?,
            args: vec!["lsp".to_string()],
            env: Default::default(),
        })
    }
}

zed::register_extension!(MdrevExtension);
