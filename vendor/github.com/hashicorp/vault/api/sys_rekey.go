package api

func (c *Sys) RekeyStatus() (*RekeyStatusResponse, error) {
	r := c.c.NewRequest("GET", "/v1/sys/rekey/init")
	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyStatusResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyRecoveryKeyStatus() (*RekeyStatusResponse, error) {
	r := c.c.NewRequest("GET", "/v1/sys/rekey-recovery-key/init")
	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyStatusResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyInit(config *RekeyInitRequest) (*RekeyStatusResponse, error) {
	r := c.c.NewRequest("PUT", "/v1/sys/rekey/init")
	if err := r.SetJSONBody(config); err != nil {
		return nil, err
	}

	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyStatusResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyRecoveryKeyInit(config *RekeyInitRequest) (*RekeyStatusResponse, error) {
	r := c.c.NewRequest("PUT", "/v1/sys/rekey-recovery-key/init")
	if err := r.SetJSONBody(config); err != nil {
		return nil, err
	}

	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyStatusResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyCancel() error {
	r := c.c.NewRequest("DELETE", "/v1/sys/rekey/init")
	resp, err := c.c.RawRequest(r)
	if err == nil {
		defer resp.Body.Close()
	}
	return err
}

func (c *Sys) RekeyRecoveryKeyCancel() error {
	r := c.c.NewRequest("DELETE", "/v1/sys/rekey-recovery-key/init")
	resp, err := c.c.RawRequest(r)
	if err == nil {
		defer resp.Body.Close()
	}
	return err
}

func (c *Sys) RekeyUpdate(shard, nonce string) (*RekeyUpdateResponse, error) {
	body := map[string]interface{}{
		"key":   shard,
		"nonce": nonce,
	}

	r := c.c.NewRequest("PUT", "/v1/sys/rekey/update")
	if err := r.SetJSONBody(body); err != nil {
		return nil, err
	}

	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyUpdateResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyRecoveryKeyUpdate(shard, nonce string) (*RekeyUpdateResponse, error) {
	body := map[string]interface{}{
		"key":   shard,
		"nonce": nonce,
	}

	r := c.c.NewRequest("PUT", "/v1/sys/rekey-recovery-key/update")
	if err := r.SetJSONBody(body); err != nil {
		return nil, err
	}

	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyUpdateResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyRetrieveBackup() (*RekeyRetrieveResponse, error) {
	r := c.c.NewRequest("GET", "/v1/sys/rekey/backup")
	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyRetrieveResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyRetrieveRecoveryBackup() (*RekeyRetrieveResponse, error) {
	r := c.c.NewRequest("GET", "/v1/sys/rekey/recovery-backup")
	resp, err := c.c.RawRequest(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result RekeyRetrieveResponse
	err = resp.DecodeJSON(&result)
	return &result, err
}

func (c *Sys) RekeyDeleteBackup() error {
	r := c.c.NewRequest("DELETE", "/v1/sys/rekey/backup")
	resp, err := c.c.RawRequest(r)
	if err == nil {
		defer resp.Body.Close()
	}

	return err
}

func (c *Sys) RekeyDeleteRecoveryBackup() error {
	r := c.c.NewRequest("DELETE", "/v1/sys/rekey/recovery-backup")
	resp, err := c.c.RawRequest(r)
	if err == nil {
		defer resp.Body.Close()
	}

	return err
}

type RekeyInitRequest struct {
	SecretShares    int      `json:"secret_shares"`
	SecretThreshold int      `json:"secret_threshold"`
	StoredShares    int      `json:"stored_shares"`
	PGPKeys         []string `json:"pgp_keys"`
	Backup          bool
}

type RekeyStatusResponse struct {
	Nonce           string   `json:"nonce"`
	Started         bool     `json:"started"`
	T               int      `json:"t"`
	N               int      `json:"n"`
	Progress        int      `json:"progress"`
	Required        int      `json:"required"`
	PGPFingerprints []string `json:"pgp_fingerprints"`
	Backup          bool     `json:"backup"`
}

type RekeyUpdateResponse struct {
	Nonce           string   `json:"nonce"`
	Complete        bool     `json:"complete"`
	Keys            []string `json:"keys"`
	KeysB64         []string `json:"keys_base64"`
	PGPFingerprints []string `json:"pgp_fingerprints"`
	Backup          bool     `json:"backup"`
}

type RekeyRetrieveResponse struct {
	Nonce   string              `json:"nonce"`
	Keys    map[string][]string `json:"keys"`
	KeysB64 map[string][]string `json:"keys_base64"`
}
