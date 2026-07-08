package build

import (
	"fmt"
	"os"
	"strings"

	"github.com/apexpack/apexpack/internal/types"
)

type javaHook struct{}

func (javaHook) PatchMelange(cfg *types.MelangeConfig, p *types.Profile, opts Options) error {
	version := resolveVersion("java", opts.LanguageVersion)
	cfg.Environment.Env = fixJavaHome(cfg.Environment.Env, version)

	isGradleFramework := strings.HasSuffix(opts.Framework, "-gradle")

	// Maven: skipped for Gradle frameworks — Gradle does not read ~/.m2/settings.xml.
	if !isGradleFramework {
		tmplName := p.Build.MavenSettingsTemplate
		if tmplName == "" {
			tmplName = "default"
		}
		if p.Build.MavenMirrorURL != "" {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD"} {
				if val := os.Getenv(key); val != "" {
					if _, exists := cfg.Environment.Env[key]; !exists {
						cfg.Environment.Env[key] = val
					}
				}
			}
			tmpl, err := loadMavenTemplate(tmplName, opts.ProfilesDir)
			if err != nil {
				return fmt.Errorf("maven settings template: %w", err)
			}
			settingsXML := strings.ReplaceAll(tmpl, "{{MAVEN_MIRROR_URL}}", p.Build.MavenMirrorURL)
			mirrorStep := fmt.Sprintf(
				"mkdir -p /home/build/.m2\n"+
					"cat > /home/build/.m2/settings.xml << APEXPACK_SETTINGS_EOF\n"+
					"%s"+
					"APEXPACK_SETTINGS_EOF\n"+
					"echo \"→ Maven settings: %s template, mirror: %s\"",
				settingsXML, tmplName, p.Build.MavenMirrorURL,
			)
			cfg.Pipeline = append(
				[]types.MelangePipeline{{Runs: mirrorStep}},
				cfg.Pipeline...,
			)
		}
	}

	// Gradle: inject init script for corporate Artifactory mirrors.
	if isGradleFramework {
		gradleTmplName := p.Build.GradleSettingsTemplate
		if gradleTmplName == "" {
			gradleTmplName = "corporate"
		}
		if p.Build.GradleMirrorURL != "" {
			if cfg.Environment.Env == nil {
				cfg.Environment.Env = make(map[string]string)
			}
			for _, key := range []string{"ARTI_USER", "ARTI_PASSWORD"} {
				if val := os.Getenv(key); val != "" {
					if _, exists := cfg.Environment.Env[key]; !exists {
						cfg.Environment.Env[key] = val
					}
				}
			}
			gradleTmpl, err := loadGradleTemplate(gradleTmplName, opts.ProfilesDir)
			if err != nil {
				return fmt.Errorf("gradle init script template: %w", err)
			}
			initScript := strings.ReplaceAll(gradleTmpl, "{{GRADLE_MIRROR_URL}}", p.Build.GradleMirrorURL)
			gradleStep := "mkdir -p /home/build/.gradle/init.d\n" +
				"cat > /home/build/.gradle/init.d/artifactory.gradle << APEXPACK_GRADLE_EOF\n" +
				initScript +
				"APEXPACK_GRADLE_EOF\n" +
				fmt.Sprintf("echo \"→ Gradle init script: %s template, mirror: %s\"", gradleTmplName, p.Build.GradleMirrorURL)
			cfg.Pipeline = append(
				[]types.MelangePipeline{{Runs: gradleStep}},
				cfg.Pipeline...,
			)
		}

		// Rewrite gradle-wrapper.properties distributionUrl when a separate
		// distribution repo is configured. The wrapper downloads its own zip from
		// services.gradle.org before any init scripts or GRADLE_OPTS apply, so it
		// bypasses cert fixes and may be blocked entirely in air-gapped clusters.
		if p.Build.GradleDistributionURL != "" {
			distBase := strings.TrimRight(p.Build.GradleDistributionURL, "/")
			wrapperStep := "if [ -f \"gradle/wrapper/gradle-wrapper.properties\" ]; then\n" +
				"  DIST_FILE=$(grep \"^distributionUrl=\" gradle/wrapper/gradle-wrapper.properties | sed 's/.*\\///')\n" +
				"  NEW_URL=\"" + distBase + "/${DIST_FILE}\"\n" +
				"  NEW_URL_ESC=$(printf '%s' \"$NEW_URL\" | sed 's/:/\\\\:/g')\n" +
				"  sed -i \"s|^distributionUrl=.*|distributionUrl=${NEW_URL_ESC}|\" gradle/wrapper/gradle-wrapper.properties\n" +
				"  echo \"→ Gradle wrapper distributionUrl → " + distBase + "\"\n" +
				"fi"
			cfg.Pipeline = append(
				[]types.MelangePipeline{{Runs: wrapperStep}},
				cfg.Pipeline...,
			)
		}
	}

	return nil
}

func (javaHook) PatchApko(cfg *types.ApkoConfig, p *types.Profile, opts Options) error {
	version := resolveVersion("java", opts.LanguageVersion)
	cfg.Environment = fixJavaHome(cfg.Environment, version)

	// If JAVA_HOME is set in the image env, derive PATH and resolve the bare
	// "java" entrypoint to a full path. This keeps JAVA_HOME as the single
	// version-pinned value — changing openjdk-21 → openjdk-17 only requires
	// updating JAVA_HOME; PATH and entrypoint follow automatically.
	if javaHome, ok := cfg.Environment["JAVA_HOME"]; ok && javaHome != "" {
		if _, hasPath := cfg.Environment["PATH"]; !hasPath {
			cfg.Environment["PATH"] = javaHome + "/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
		}
		if cfg.Entrypoint.Command == "java" {
			cfg.Entrypoint.Command = javaHome + "/bin/java"
		}
	}

	return nil
}

// javaHomeDirVersion returns the JVM directory version for JAVA_HOME paths.
// Java 8 uses the pre-9 "1.N" naming convention in its directory name:
//
//	openjdk-8-jre  →  /usr/lib/jvm/java-1.8-openjdk  (Wolfi convention)
//	openjdk-17-jre →  /usr/lib/jvm/java-17-openjdk
func javaHomeDirVersion(major string) string {
	if major == "8" {
		return "1.8"
	}
	return major
}

// fixJavaHome corrects JAVA_HOME paths after {JAVA_VERSION} token substitution.
// For Java 8, Wolfi installs the JRE at java-1.8-openjdk, not java-8-openjdk.
// This only touches the JAVA_HOME key; all other env vars are passed through unchanged.
func fixJavaHome(env map[string]string, version string) map[string]string {
	if env == nil {
		return env
	}
	dirVer := javaHomeDirVersion(version)
	if dirVer == version {
		return env // no correction needed
	}
	wrong := "/java-" + version + "-openjdk"
	right := "/java-" + dirVer + "-openjdk"
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = strings.ReplaceAll(v, wrong, right)
	}
	return out
}
