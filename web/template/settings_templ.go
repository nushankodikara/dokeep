// Code generated by templ - DO NOT EDIT.

// templ: version: v0.3.920
package template

//lint:file-ignore SA4006 This context is only used if a nested component is present.

import "github.com/a-h/templ"
import templruntime "github.com/a-h/templ/runtime"

func SettingsPage() templ.Component {
	return templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
		templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
		if templ_7745c5c3_CtxErr := ctx.Err(); templ_7745c5c3_CtxErr != nil {
			return templ_7745c5c3_CtxErr
		}
		templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
		if !templ_7745c5c3_IsBuffer {
			defer func() {
				templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
				if templ_7745c5c3_Err == nil {
					templ_7745c5c3_Err = templ_7745c5c3_BufErr
				}
			}()
		}
		ctx = templ.InitializeContext(ctx)
		templ_7745c5c3_Var1 := templ.GetChildren(ctx)
		if templ_7745c5c3_Var1 == nil {
			templ_7745c5c3_Var1 = templ.NopComponent
		}
		ctx = templ.ClearChildren(ctx)
		templ_7745c5c3_Var2 := templruntime.GeneratedTemplate(func(templ_7745c5c3_Input templruntime.GeneratedComponentInput) (templ_7745c5c3_Err error) {
			templ_7745c5c3_W, ctx := templ_7745c5c3_Input.Writer, templ_7745c5c3_Input.Context
			templ_7745c5c3_Buffer, templ_7745c5c3_IsBuffer := templruntime.GetBuffer(templ_7745c5c3_W)
			if !templ_7745c5c3_IsBuffer {
				defer func() {
					templ_7745c5c3_BufErr := templruntime.ReleaseBuffer(templ_7745c5c3_Buffer)
					if templ_7745c5c3_Err == nil {
						templ_7745c5c3_Err = templ_7745c5c3_BufErr
					}
				}()
			}
			ctx = templ.InitializeContext(ctx)
			templ_7745c5c3_Err = templruntime.WriteString(templ_7745c5c3_Buffer, 1, "<h3 class=\"text-3xl font-medium text-gray-700\">User Settings</h3><div class=\"mt-8\"><div class=\"mt-6\"><div class=\"px-4 py-5 bg-white shadow sm:p-6\"><div class=\"md:grid md:grid-cols-3 md:gap-6\"><div class=\"md:col-span-1\"><h3 class=\"text-lg font-medium leading-6 text-gray-900\">Change Password</h3><p class=\"mt-1 text-sm text-gray-600\">Update your password to a new one.</p></div><div class=\"mt-5 md:mt-0 md:col-span-2\"><form action=\"/settings/password\" method=\"POST\"><div class=\"grid grid-cols-6 gap-6\"><div class=\"col-span-6 sm:col-span-4\"><label for=\"current_password\" class=\"block text-sm font-medium text-gray-700\">Current Password</label> <input type=\"password\" name=\"current_password\" id=\"current_password\" class=\"mt-1 block w-full border border-gray-300 rounded-md shadow-sm py-2 px-3 focus:outline-none focus:ring-indigo-500 focus:border-indigo-500 sm:text-sm\"></div><div class=\"col-span-6 sm:col-span-4\"><label for=\"new_password\" class=\"block text-sm font-medium text-gray-700\">New Password</label> <input type=\"password\" name=\"new_password\" id=\"new_password\" class=\"mt-1 block w-full border border-gray-300 rounded-md shadow-sm py-2 px-3 focus:outline-none focus:ring-indigo-500 focus:border-indigo-500 sm:text-sm\"></div></div><div class=\"mt-6\"><button type=\"submit\" class=\"inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500\">Save</button></div></form></div></div></div><div class=\"mt-6\"><div class=\"px-4 py-5 bg-white shadow sm:p-6\"><div class=\"md:grid md:grid-cols-3 md:gap-6\"><div class=\"md:col-span-1\"><h3 class=\"text-lg font-medium leading-6 text-gray-900\">Two-Factor Authentication</h3><p class=\"mt-1 text-sm text-gray-600\">Add an additional layer of security to your account.</p></div><div class=\"mt-5 md:mt-0 md:col-span-2\"><a href=\"/setup-totp\" class=\"inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-green-600 hover:bg-green-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-green-500\">Enable 2FA</a></div></div></div></div></div></div>")
			if templ_7745c5c3_Err != nil {
				return templ_7745c5c3_Err
			}
			return nil
		})
		templ_7745c5c3_Err = Layout("User Settings").Render(templ.WithChildren(ctx, templ_7745c5c3_Var2), templ_7745c5c3_Buffer)
		if templ_7745c5c3_Err != nil {
			return templ_7745c5c3_Err
		}
		return nil
	})
}

var _ = templruntime.GeneratedTemplate
