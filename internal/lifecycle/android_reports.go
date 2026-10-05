package lifecycle

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const cleartextTest = "cleartext_share_authentication_uses_token_fallback"

func (w *workflow) verifyAndroidCompletion(ctx context.Context, output []byte, env map[string]string) error {
	var profile string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "samples_path=") {
			profile = filepath.Dir(strings.TrimPrefix(line, "samples_path="))
		}
	}
	if profile == "" {
		return fmt.Errorf("android harness did not report a resource profile")
	}
	rel, err := filepath.Rel(filepath.Join(w.root, "android", "test-artifacts"), profile)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("android profile is outside owned test artifacts")
	}
	wantedClasses := map[string]bool{}
	if env["LINGON_IT_CLASS"] != "" {
		for _, name := range strings.Split(env["LINGON_IT_CLASS"], ",") {
			wantedClasses[name] = true
		}
	}
	expected := make(map[string]bool)
	err = filepath.WalkDir(filepath.Join(w.root, "android", "app", "src", "androidTest"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".kt") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		pkg, cls := kotlinPackage.FindSubmatch(data), kotlinClass.FindSubmatch(data)
		if len(pkg) != 2 || len(cls) != 2 {
			return nil
		}
		class := string(pkg[1]) + "." + string(cls[1])
		if len(wantedClasses) > 0 {
			if !wantedClasses[class] {
				return nil
			}
		} else if string(cls[1]) != "EndToEndTest" {
			return nil
		}
		// Use the same tested source parser as the Android harness, not a
		// second release-specific interpretation of Kotlin @Test methods.
		out, err := w.exec(ctx, filepath.Join(w.root, "android"), "go", "run", "./cmd/lingon-android-tools", "test-names", "--file", path)
		if err != nil {
			return err
		}
		for _, method := range strings.Fields(string(out)) {
			if env["LINGON_IT_ONLY"] != "" && method != env["LINGON_IT_ONLY"] {
				continue
			}
			expected[class+"#"+method] = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(expected) == 0 {
		return fmt.Errorf("no required Android test methods discovered")
	}
	files, err := filepath.Glob(filepath.Join(profile, "android-test-batches", "*", "TEST-*.xml"))
	if err != nil || len(files) == 0 {
		return fmt.Errorf("missing per-batch Android JUnit evidence")
	}
	allowedSkipped := make(map[string]bool)
	if env["LINGON_IT_CLEARTEXT"] == "0" && env["LINGON_IT_CLASS"] == "" {
		for key := range expected {
			if strings.HasSuffix(key, "#"+cleartextTest) {
				allowedSkipped[key] = true
			}
		}
	}
	return checkAndroidReports(files, expected, allowedSkipped)
}

func checkAndroidReports(files []string, expected, allowedSkipped map[string]bool) error {
	seen := make(map[string]bool)
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		err = func() error {
			defer file.Close()
			decoder := xml.NewDecoder(file)
			current := ""
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				switch tok := token.(type) {
				case xml.StartElement:
					if tok.Name.Local == "testsuite" {
						for _, attr := range tok.Attr {
							if (attr.Name.Local == "errors" || attr.Name.Local == "failures") && attr.Value != "0" {
								return fmt.Errorf("android suite reports %s=%s", attr.Name.Local, attr.Value)
							}
						}
					}
					if tok.Name.Local == "testcase" {
						var class, name string
						for _, attr := range tok.Attr {
							if attr.Name.Local == "classname" {
								class = attr.Value
							}
							if attr.Name.Local == "name" {
								name = attr.Value
							}
						}
						current = class + "#" + name
						if !expected[current] || seen[current] {
							return fmt.Errorf("unexpected or duplicate Android result: %s", current)
						}
						seen[current] = true
					}
					if tok.Name.Local == "failure" || tok.Name.Local == "error" {
						return fmt.Errorf("android test failed: %s", current)
					}
					if tok.Name.Local == "skipped" && !allowedSkipped[current] {
						return fmt.Errorf("required Android test skipped: %s", current)
					}
				case xml.EndElement:
					if tok.Name.Local == "testcase" {
						current = ""
					}
				}
			}
		}()
		if err != nil {
			return err
		}
	}
	for key := range expected {
		if !seen[key] {
			return fmt.Errorf("required Android result is missing: %s", key)
		}
	}
	return nil
}
