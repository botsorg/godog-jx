package cmd

import (
	"fmt"
	"io"

	"github.com/jenkins-x/jx/pkg/addon"
	"github.com/jenkins-x/jx/pkg/auth"
	"github.com/jenkins-x/jx/pkg/jx/cmd/templates"
	cmdutil "github.com/jenkins-x/jx/pkg/jx/cmd/util"
	"github.com/jenkins-x/jx/pkg/kube"
	"github.com/jenkins-x/jx/pkg/util"
	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	createTokenAddonLong = templates.LongDesc(`
		Creates a new User Token for an Addon service
`)

	createTokenAddonExample = templates.Examples(`
		# Add a new User Token for an addonservice
		jx create token addon -n anchore someUserName

		# As above with the password being passed in
		jx create token addon -n anchore -p somePassword someUserName	
	`)
)

// CreateTokenAddonOptions the command line options for the command
type CreateTokenAddonOptions struct {
	CreateOptions

	ServerFlags ServerFlags
	Username    string
	Password    string
	ApiToken    string
	Timeout     string
	Kind        string
}

// NewCmdCreateTokenAddon creates a command
func NewCmdCreateTokenAddon(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := &CreateTokenAddonOptions{
		CreateOptions: CreateOptions{
			CommonOptions: CommonOptions{
				Factory: f,
				Out:     out,
				Err:     errOut,
			},
		},
	}

	cmd := &cobra.Command{
		Use:     "addon [username]",
		Short:   "Adds a new token/login for a user for a given addon",
		Aliases: []string{"login"},
		Long:    createTokenAddonLong,
		Example: createTokenAddonExample,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := options.Run()
			cmdutil.CheckErr(err)
		},
	}
	options.addCommonFlags(cmd)
	options.ServerFlags.addGitServerFlags(cmd)
	cmd.Flags().StringVarP(&options.Password, "password", "p", "", "The password for the user")
	cmd.Flags().StringVarP(&options.ApiToken, "api-token", "t", "", "The API Token for the user")
	cmd.Flags().StringVarP(&options.Timeout, "timeout", "", "", "The timeout if using browser automation to generate the API token (by passing username and password)")
	cmd.Flags().StringVarP(&options.Kind, "kind", "k", "", "The kind of addon. Defaults to the addon name if not specified")

	return cmd
}

// Run implements the command
func (o *CreateTokenAddonOptions) Run() error {
	args := o.Args
	if len(args) > 0 {
		o.Username = args[0]
	}
	if len(args) > 1 {
		o.ApiToken = args[1]
	}
	authConfigSvc, err := o.CreateAddonAuthConfigService()
	if err != nil {
		return err
	}
	config := authConfigSvc.Config()
	kind := o.Kind
	if kind == "" {
		kind = o.ServerFlags.ServerName
	}
	if kind == "" {
		kind = "addon"
	}

	var server *auth.AuthServer
	server, err = o.findAddonServer(config, &o.ServerFlags, kind)
	if err != nil {
		return err
	}
	if o.Username == "" {
		return fmt.Errorf("No Username specified")
	}
	userAuth := config.GetOrCreateUserAuth(server.URL, o.Username)
	if o.ApiToken != "" {
		userAuth.ApiToken = o.ApiToken
	}

	if o.Password != "" {
		userAuth.Password = o.Password
	} else {
		tokenUrl := addon.ProviderAccessTokenURL(server.Kind, server.URL)
		if userAuth.IsInvalid() {
			f := func(username string) error {
				o.Printf("Please generate an API Token for %s server %s\n", server.Kind, server.Label())
				if tokenUrl != "" {
					o.Printf("Click this URL %s\n\n", util.ColorInfo(tokenUrl))
				}
				o.Printf("Then COPY the token and enter in into the form below:\n\n")
				return nil
			}

			err = config.EditUserAuth(server.Label(), userAuth, o.Username, false, o.BatchMode, f)
			if err != nil {
				return err
			}
			if userAuth.IsInvalid() {
				return fmt.Errorf("You did not properly define the user authentication!")
			}
		}
	}

	config.CurrentServer = server.URL
	err = authConfigSvc.SaveConfig()
	if err != nil {
		return err
	}
	err = o.updateAddonCredentialsSecret(server, userAuth)
	if err != nil {
		o.warnf("Failed to update addon credentials secret: %v\n", err)
	}
	o.Printf("Created user %s API Token for addon server %s at %s\n",
		util.ColorInfo(o.Username), util.ColorInfo(server.Name), util.ColorInfo(server.URL))
	return nil
}

func (o *CreateTokenAddonOptions) updateAddonCredentialsSecret(server *auth.AuthServer, userAuth *auth.UserAuth) error {
	client, curNs, err := o.Factory.CreateClient()
	if err != nil {
		return err
	}
	ns, _, err := kube.GetDevNamespace(client, curNs)
	if err != nil {
		return err
	}
	options := metav1.GetOptions{}
	name := kube.ToValidName(kube.SecretJenkinsPipelineAddonCredentials + server.Kind + "-" + server.Name)
	secrets := client.CoreV1().Secrets(ns)
	secret, err := secrets.Get(name, options)
	create := false
	operation := "update"
	labels := map[string]string{
		kube.LabelCredentialsType: kube.ValueCredentialTypeUsernamePassword,
		kube.LabelCreatedBy:       kube.ValueCreatedByJX,
		kube.LabelKind:            kube.ValueKindAddon,
		kube.LabelServiceKind:     server.Kind,
	}
	annotations := map[string]string{
		kube.AnnotationCredentialsDescription: fmt.Sprintf("API Token for acccessing %s addon inside pipelines", server.URL),
		kube.AnnotationURL:                    server.URL,
		kube.AnnotationName:                   server.Name,
	}
	if err != nil {
		// lets create a new secret
		create = true
		operation = "create"
		secret = &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        name,
				Annotations: annotations,
				Labels:      labels,
			},
			Data: map[string][]byte{},
		}
	} else {
		secret.Annotations = kube.MergeMaps(secret.Annotations, annotations)
		secret.Labels = kube.MergeMaps(secret.Labels, labels)
	}
	if userAuth.Username != "" {
		secret.Data["username"] = []byte(userAuth.Username)
	}
	if userAuth.Password != "" {
		secret.Data["password"] = []byte(userAuth.Password)
	}
	if userAuth.ApiToken != "" {
		secret.Data["password"] = []byte(userAuth.ApiToken)
	}
	if create {
		_, err = secrets.Create(secret)
	} else {
		_, err = secrets.Update(secret)
	}
	if err != nil {
		return fmt.Errorf("Failed to %s secret %s due to %s", operation, secret.Name, err)
	}
	return nil
}
